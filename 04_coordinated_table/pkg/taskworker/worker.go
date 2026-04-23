package taskworker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"

	"async-tasks-ydb/04_coordinated_table/pkg/metrics"
	"async-tasks-ydb/04_coordinated_table/pkg/rebalancer"
	"async-tasks-ydb/04_coordinated_table/pkg/uid"
)

type lockedTask struct {
	id          string
	partitionID uint16
	priority    uint8
	lockValue   string
}

// Worker processes tasks from owned partitions.
type Worker struct {
	DB           *ydb.Driver
	WorkerID     string
	LockDuration time.Duration
	BackoffMin   time.Duration
	BackoffMax   time.Duration
	Stats        *metrics.Stats
}

// Run listens for partition events and manages per-partition goroutines.
func (w *Worker) Run(ctx context.Context, partitionCh <-chan rebalancer.PartitionEvent) {
	type partitionState struct {
		cancel context.CancelFunc
		done   chan struct{}
	}

	partitions := make(map[int]*partitionState)
	var mu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			for _, ps := range partitions {
				ps.cancel()
			}
			mu.Unlock()
			mu.Lock()
			dones := make([]chan struct{}, 0, len(partitions))
			for _, ps := range partitions {
				dones = append(dones, ps.done)
			}
			mu.Unlock()
			for _, done := range dones {
				<-done
			}
			return

		case evt, ok := <-partitionCh:
			if !ok {
				mu.Lock()
				for _, ps := range partitions {
					ps.cancel()
				}
				mu.Unlock()
				mu.Lock()
				dones := make([]chan struct{}, 0, len(partitions))
				for _, ps := range partitions {
					dones = append(dones, ps.done)
				}
				mu.Unlock()
				for _, done := range dones {
					<-done
				}
				return
			}

			if evt.Lease != nil {
				partCtx, cancel := context.WithCancel(ctx)
				done := make(chan struct{})

				mu.Lock()
				partitions[evt.PartitionID] = &partitionState{cancel: cancel, done: done}
				mu.Unlock()

				w.Stats.Partitions.Add(1)
				slog.Info("worker started", "worker_id", w.WorkerID, "partitions_owned", metrics.ReadGauge(w.Stats.Partitions))

				go func(partitionID int, leaseCtx context.Context) {
					defer close(done)
					defer func() {
						w.Stats.Partitions.Add(-1)
						mu.Lock()
						delete(partitions, partitionID)
						mu.Unlock()
					}()
					w.processPartition(partCtx, leaseCtx, partitionID)
				}(evt.PartitionID, evt.Lease.Context())

			} else {
				mu.Lock()
				ps, exists := partitions[evt.PartitionID]
				mu.Unlock()
				if exists {
					ps.cancel()
					<-ps.done
				}
			}
		}
	}
}

func (w *Worker) processPartition(ctx context.Context, leaseCtx context.Context, partitionID int) {
	backoff := w.BackoffMin

	for {
		select {
		case <-ctx.Done():
			return
		case <-leaseCtx.Done():
			slog.Info("partition lease lost", "worker_id", w.WorkerID, "partition_id", partitionID)
			return
		default:
		}

		task, err := w.lockNextTask(ctx, partitionID)
		if err != nil {
			if ctx.Err() != nil || leaseCtx.Err() != nil {
				return
			}
			w.Stats.Errors.Add(1)
			slog.Warn("lock task failed", "worker_id", w.WorkerID, "partition_id", partitionID, "err", err)
			w.sleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}

		if task == nil {
			w.sleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}

		backoff = w.BackoffMin
		w.Stats.Locked.Add(1)
		slog.Info("task locked",
			"worker_id", w.WorkerID,
			"partition_id", task.partitionID,
			"task_id", task.id,
			"priority", task.priority,
		)

		go w.completeTask(context.Background(), task)
	}
}

// lockNextTask selects and locks the highest-priority eligible task in a partition.
// Returns nil, nil if no eligible task exists.
func (w *Worker) lockNextTask(ctx context.Context, partitionID int) (*lockedTask, error) {
	lockValue, err := uid.GenerateUUID()
	if err != nil {
		return nil, err
	}
	lockedUntil := time.Now().UTC().Add(w.LockDuration)

	var result *lockedTask

	err = w.DB.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		result = nil

		rs, err := tx.Query(ctx,
			`DECLARE $partition_id AS Uint16;
SELECT id, priority
FROM coordinated_tasks
WHERE partition_id = $partition_id
  AND (
      status = 'pending'
      OR (status = 'locked' AND locked_until < CurrentUtcTimestamp())
  )
  AND (scheduled_at IS NULL OR scheduled_at <= CurrentUtcTimestamp())
ORDER BY priority DESC
LIMIT 1;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$partition_id").Uint16(uint16(partitionID)).
					Build(),
			),
		)
		if err != nil {
			return fmt.Errorf("select task: %w", err)
		}
		defer rs.Close(ctx) //nolint:errcheck

		resultSet, err := rs.NextResultSet(ctx)
		if err != nil {
			return fmt.Errorf("next result set: %w", err)
		}

		row, err := resultSet.NextRow(ctx)
		if err != nil {
			return nil
		}

		var taskID string
		var priority uint8
		if err := row.ScanNamed(
			query.Named("id", &taskID),
			query.Named("priority", &priority),
		); err != nil {
			return fmt.Errorf("scan task row: %w", err)
		}

		if err := tx.Exec(ctx,
			`DECLARE $id AS Utf8;
DECLARE $partition_id AS Uint16;
DECLARE $lock_value AS Utf8;
DECLARE $locked_until AS Timestamp;
UPDATE coordinated_tasks
SET status = 'locked',
    lock_value = $lock_value,
    locked_until = $locked_until
WHERE partition_id = $partition_id
  AND id = $id;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(taskID).
					Param("$partition_id").Uint16(uint16(partitionID)).
					Param("$lock_value").Text(lockValue).
					Param("$locked_until").Timestamp(lockedUntil).
					Build(),
			),
		); err != nil {
			return fmt.Errorf("update task lock: %w", err)
		}

		result = &lockedTask{
			id:          taskID,
			partitionID: uint16(partitionID),
			priority:    priority,
			lockValue:   lockValue,
		}
		return nil
	}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))

	if err != nil {
		return nil, err
	}
	return result, nil
}

func (w *Worker) completeTask(ctx context.Context, task *lockedTask) {
	time.Sleep(100 * time.Millisecond)

	doneAt := time.Now().UTC()
	err := w.DB.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		return tx.Exec(ctx,
			`DECLARE $id AS Utf8;
DECLARE $partition_id AS Uint16;
DECLARE $done_at AS Timestamp;
UPDATE coordinated_tasks
SET status = 'completed',
    done_at = $done_at
WHERE partition_id = $partition_id
  AND id = $id;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(task.id).
					Param("$partition_id").Uint16(task.partitionID).
					Param("$done_at").Timestamp(doneAt).
					Build(),
			),
		)
	}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))

	if err != nil {
		w.Stats.Errors.Add(1)
		slog.Warn("task complete failed",
			"worker_id", w.WorkerID,
			"task_id", task.id,
			"err", err,
		)
		return
	}

	w.Stats.Processed.Add(1)
	slog.Info("task completed",
		"worker_id", w.WorkerID,
		"partition_id", task.partitionID,
		"task_id", task.id,
	)
}

func (w *Worker) sleep(ctx context.Context, leaseCtx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-leaseCtx.Done():
	case <-timer.C:
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
