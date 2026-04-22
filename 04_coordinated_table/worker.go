package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
)

// lockedTask holds the data of a task that was successfully locked.
type lockedTask struct {
	id          string
	partitionID uint16
	priority    uint8
	lockValue   string
}

// Worker processes tasks from owned partitions.
type Worker struct {
	db           *ydb.Driver
	workerID     string
	lockDuration time.Duration
	backoffMin   time.Duration
	backoffMax   time.Duration
	stats        *Stats
}

// run listens for partition events and manages per-partition goroutines.
func (w *Worker) run(ctx context.Context, partitionCh <-chan partitionEvent) {
	type partitionState struct {
		cancel context.CancelFunc
		done   chan struct{}
	}

	partitions := make(map[int]*partitionState)
	var mu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			// Cancel all partition goroutines and wait for them.
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
				// Channel closed — rebalancer shut down.
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

			if evt.lease != nil {
				// Partition acquired — start processing goroutine.
				partCtx, cancel := context.WithCancel(ctx)
				done := make(chan struct{})

				mu.Lock()
				partitions[evt.partitionID] = &partitionState{cancel: cancel, done: done}
				mu.Unlock()

				w.stats.partitions.Add(1)
				slog.Info("worker started", "worker_id", w.workerID, "partitions_owned", readGauge(w.stats.partitions))

				go func(partitionID int, leaseCtx context.Context) {
					defer close(done)
					defer func() {
						w.stats.partitions.Add(-1)
						mu.Lock()
						delete(partitions, partitionID)
						mu.Unlock()
					}()
					w.processPartition(partCtx, leaseCtx, partitionID)
				}(evt.partitionID, evt.lease.Context())

			} else {
				// Partition released by rebalancer — cancel its goroutine and wait.
				mu.Lock()
				ps, exists := partitions[evt.partitionID]
				mu.Unlock()
				if exists {
					ps.cancel()
					<-ps.done
				}
			}
		}
	}
}

// processPartition polls and processes tasks for a single partition until the context or lease is done.
func (w *Worker) processPartition(ctx context.Context, leaseCtx context.Context, partitionID int) {
	backoff := w.backoffMin

	for {
		// Exit if our partition context or lease context is done.
		select {
		case <-ctx.Done():
			return
		case <-leaseCtx.Done():
			slog.Info("partition lease lost", "worker_id", w.workerID, "partition_id", partitionID)
			return
		default:
		}

		task, err := w.lockNextTask(ctx, partitionID)
		if err != nil {
			if ctx.Err() != nil || leaseCtx.Err() != nil {
				return
			}
			w.stats.errors.Add(1)
			slog.Warn("lock task failed", "worker_id", w.workerID, "partition_id", partitionID, "err", err)
			w.sleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.backoffMax)
			continue
		}

		if task == nil {
			// No eligible task — back off.
			w.sleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.backoffMax)
			continue
		}

		// Task locked — reset backoff and process asynchronously.
		backoff = w.backoffMin
		w.stats.locked.Add(1)
		slog.Info("task locked",
			"worker_id", w.workerID,
			"partition_id", task.partitionID,
			"task_id", task.id,
			"priority", task.priority,
		)

		// Simulate 100ms work then mark complete.
		go w.completeTask(context.Background(), task)
	}
}

// lockNextTask selects and locks the highest-priority eligible task in a partition.
// Returns nil, nil if no eligible task exists.
//
// Eligibility (US4 + US5):
//   - status = 'pending'  OR  (status = 'locked' AND locked_until < CurrentUtcTimestamp())
//   - scheduled_at IS NULL  OR  scheduled_at <= CurrentUtcTimestamp()
//
// ORDER BY priority DESC picks the most urgent task first.
func (w *Worker) lockNextTask(ctx context.Context, partitionID int) (*lockedTask, error) {
	lockValue, err := generateUUID()
	if err != nil {
		return nil, err
	}
	lockedUntil := time.Now().UTC().Add(w.lockDuration)

	var result *lockedTask

	err = w.db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		result = nil

		// SELECT the highest-priority eligible task.
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
			// No rows — no eligible task.
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

		// UPDATE to lock the task.
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

// completeTask simulates 100ms of work then marks the task completed.
func (w *Worker) completeTask(ctx context.Context, task *lockedTask) {
	time.Sleep(100 * time.Millisecond)

	doneAt := time.Now().UTC()
	err := w.db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
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
		w.stats.errors.Add(1)
		slog.Warn("task complete failed",
			"worker_id", w.workerID,
			"task_id", task.id,
			"err", err,
		)
		return
	}

	w.stats.processed.Add(1)
	slog.Info("task completed",
		"worker_id", w.workerID,
		"partition_id", task.partitionID,
		"task_id", task.id,
	)
}

// sleep waits for duration d, returning early if ctx or leaseCtx is done.
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
