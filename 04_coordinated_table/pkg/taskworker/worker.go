package taskworker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"async-tasks-ydb/04_coordinated_table/pkg/metrics"
	"async-tasks-ydb/04_coordinated_table/pkg/rebalancer"
	"async-tasks-ydb/04_coordinated_table/pkg/uid"
)

// Worker processes tasks from owned partitions.
type Worker struct {
	Repo         TaskRepository
	WorkerID     string
	LockDuration time.Duration
	BackoffMin   time.Duration
	BackoffMax   time.Duration
	Stats        *metrics.Stats
	ProcessTask  func(ctx context.Context, taskID string, payload string) error
	// SleepFn replaces the real timer sleep when set; used in tests to record
	// backoff durations and return immediately.
	SleepFn func(d time.Duration)
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

		candidate, err := w.Repo.FetchEligibleCandidate(ctx, uint16(partitionID))
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			w.Stats.Errors.Add(1)
			slog.Warn("lock task failed", "worker_id", w.WorkerID, "partition_id", partitionID, "err", err)
			w.doSleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}

		if candidate == nil {
			w.doSleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}

		lockValue, err := uid.GenerateUUID()
		if err != nil {
			w.Stats.Errors.Add(1)
			slog.Warn("lock task failed", "worker_id", w.WorkerID, "partition_id", partitionID, "err", err)
			w.doSleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}
		lockedUntil := time.Now().UTC().Add(w.LockDuration)

		claimed, err := w.Repo.ClaimTask(ctx, uint16(partitionID), *candidate, lockValue, lockedUntil)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			w.Stats.Errors.Add(1)
			slog.Warn("lock task failed", "worker_id", w.WorkerID, "partition_id", partitionID, "err", err)
			w.doSleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}

		if claimed == nil {
			continue
		}

		backoff = w.BackoffMin
		w.Stats.Locked.Add(1)
		slog.Info("task locked",
			"worker_id", w.WorkerID,
			"partition_id", claimed.PartitionID,
			"task_id", claimed.ID,
			"priority", claimed.Priority,
		)

		if w.ProcessTask != nil {
			if err := w.ProcessTask(ctx, claimed.ID, claimed.Payload); err != nil {
				w.Stats.Errors.Add(1)
				slog.Warn("task processor failed", "worker_id", w.WorkerID, "task_id", claimed.ID, "err", err)
				continue
			}
		}

		doneAt := time.Now().UTC()
		if err := w.Repo.MarkCompleted(ctx, *claimed, doneAt); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			w.Stats.Errors.Add(1)
			slog.Warn("task complete failed", "worker_id", w.WorkerID, "task_id", claimed.ID, "err", err)
			continue
		}

		w.Stats.Processed.Add(1)
		slog.Info("task completed",
			"worker_id", w.WorkerID,
			"partition_id", claimed.PartitionID,
			"task_id", claimed.ID,
		)
	}
}

func (w *Worker) doSleep(ctx context.Context, leaseCtx context.Context, d time.Duration) {
	if w.SleepFn != nil {
		w.SleepFn(d)
		return
	}
	w.sleep(ctx, leaseCtx, d)
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
