package taskworker

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"async-tasks-ydb/05_ordered_tasks/pkg/metrics"
	"async-tasks-ydb/05_ordered_tasks/pkg/rebalancer"
	"async-tasks-ydb/05_ordered_tasks/pkg/uid"
)

// Worker processes ordered tasks from owned partitions, dispatching only the
// head of each entity per scan iteration.
type Worker struct {
	Repo         TaskRepository
	WorkerID     string
	LockDuration time.Duration
	BackoffMin   time.Duration
	BackoffMax   time.Duration
	MaxAttempts  uint32
	FetchK       int
	Stats        *metrics.Stats
	ProcessTask  func(ctx context.Context, task ClaimedTask) error
	// SleepFn replaces the real timer sleep when set; used in tests.
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

	drainAndWait := func() {
		mu.Lock()
		for _, ps := range partitions {
			ps.cancel()
		}
		dones := make([]chan struct{}, 0, len(partitions))
		for _, ps := range partitions {
			dones = append(dones, ps.done)
		}
		mu.Unlock()
		for _, done := range dones {
			<-done
		}
	}

	for {
		select {
		case <-ctx.Done():
			drainAndWait()
			return

		case evt, ok := <-partitionCh:
			if !ok {
				drainAndWait()
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
	k := w.FetchK
	if k <= 0 {
		k = 32
	}
	maxAttempts := w.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 10
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-leaseCtx.Done():
			slog.Info("partition lease lost", "worker_id", w.WorkerID, "partition_id", partitionID)
			return
		default:
		}

		candidates, err := w.Repo.FetchEligibleHeads(ctx, uint16(partitionID), k)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			w.Stats.Errors.Add(1)
			slog.Warn("fetch heads failed", "worker_id", w.WorkerID, "partition_id", partitionID, "err", err)
			w.doSleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}

		now := time.Now().UTC()
		heads := dedupHeads(candidates)
		dispatchable := filterDispatchable(heads, now, w.Stats)

		if len(dispatchable) == 0 {
			w.doSleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}

		// Round-robin / random pick across entities to avoid starvation.
		picked := dispatchable[rand.Intn(len(dispatchable))]

		lockValue, err := uid.GenerateUUID()
		if err != nil {
			w.Stats.Errors.Add(1)
			slog.Warn("uuid generation failed", "worker_id", w.WorkerID, "partition_id", partitionID, "err", err)
			w.doSleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}
		lockedUntil := time.Now().UTC().Add(w.LockDuration)

		claimed, err := w.Repo.ClaimTask(ctx, uint16(partitionID), picked, lockValue, lockedUntil)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			w.Stats.Errors.Add(1)
			slog.Warn("claim task failed", "worker_id", w.WorkerID, "partition_id", partitionID, "err", err)
			w.doSleep(ctx, leaseCtx, backoff)
			backoff = minDuration(backoff*2, w.BackoffMax)
			continue
		}
		if claimed == nil {
			// Lost CAS race; immediate retry without backoff.
			continue
		}

		backoff = w.BackoffMin
		w.Stats.Locked.Add(1)
		w.Stats.ClearBlocked(claimed.EntityID)
		slog.Info("task locked",
			"worker_id", w.WorkerID,
			"partition_id", claimed.PartitionID,
			"task_id", claimed.ID,
			"entity_id", claimed.EntityID,
			"entity_seq", claimed.EntitySeq,
			"attempt_count", claimed.AttemptCount,
		)

		var procErr error
		if w.ProcessTask != nil {
			procErr = w.ProcessTask(ctx, *claimed)
		}

		if procErr != nil {
			w.Stats.Errors.Add(1)
			nextAttempt := claimed.AttemptCount + 1
			if nextAttempt >= maxAttempts {
				slog.Warn("task terminally failed",
					"worker_id", w.WorkerID,
					"task_id", claimed.ID,
					"entity_id", claimed.EntityID,
					"entity_seq", claimed.EntitySeq,
					"attempt_count", nextAttempt,
					"err", procErr,
				)
				if err := w.Repo.MarkTerminallyFailed(ctx, *claimed, time.Now().UTC(), procErr.Error()); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					slog.Warn("mark terminally failed write failed", "worker_id", w.WorkerID, "task_id", claimed.ID, "err", err)
				} else {
					w.Stats.Failed.Add(1)
					w.Stats.RecordBlocked(claimed.EntityID, "terminal")
				}
				continue
			}
			retryAt := time.Now().UTC().Add(nextBackoffDelay(claimed.AttemptCount, w.BackoffMin, w.BackoffMax))
			slog.Info("task processor failed, scheduling retry",
				"worker_id", w.WorkerID,
				"task_id", claimed.ID,
				"entity_id", claimed.EntityID,
				"entity_seq", claimed.EntitySeq,
				"attempt_count", nextAttempt,
				"retry_at", retryAt,
				"err", procErr,
			)
			if err := w.Repo.MarkFailedWithBackoff(ctx, *claimed, retryAt, procErr.Error()); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				slog.Warn("mark failed with backoff write failed", "worker_id", w.WorkerID, "task_id", claimed.ID, "err", err)
			} else {
				w.Stats.Backoffs.Add(1)
				w.Stats.RecordBlocked(claimed.EntityID, "backoff")
			}
			continue
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
			"entity_id", claimed.EntityID,
			"entity_seq", claimed.EntitySeq,
		)
	}
}

// dedupHeads keeps the first row seen for each entity_id from a list ordered by
// (entity_id, entity_seq); subsequent rows for the same entity are discarded.
func dedupHeads(rows []Candidate) []Candidate {
	if len(rows) == 0 {
		return nil
	}
	out := make([]Candidate, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, c := range rows {
		if _, ok := seen[c.EntityID]; ok {
			continue
		}
		seen[c.EntityID] = struct{}{}
		out = append(out, c)
	}
	return out
}

// filterDispatchable drops heads whose backoff window hasn't elapsed and heads
// currently locked by another worker with an unexpired lease. Backed-off heads
// are stamped on the metrics surface so blocked entities are observable.
func filterDispatchable(heads []Candidate, now time.Time, stats *metrics.Stats) []Candidate {
	out := make([]Candidate, 0, len(heads))
	for _, c := range heads {
		if c.ScheduledAt != nil && c.ScheduledAt.After(now) {
			if stats != nil {
				stats.RecordBlocked(c.EntityID, "backoff")
			}
			slog.Debug("entity blocked: backoff",
				"entity_id", c.EntityID,
				"entity_seq", c.EntitySeq,
				"scheduled_at", *c.ScheduledAt,
			)
			continue
		}
		if c.Status == "locked" && c.LockedUntil != nil && c.LockedUntil.After(now) {
			slog.Debug("entity head currently locked elsewhere",
				"entity_id", c.EntityID,
				"entity_seq", c.EntitySeq,
				"locked_until", *c.LockedUntil,
			)
			continue
		}
		out = append(out, c)
	}
	return out
}

func nextBackoffDelay(attemptCount uint32, min, max time.Duration) time.Duration {
	if min <= 0 {
		min = 50 * time.Millisecond
	}
	if max <= 0 || max < min {
		max = min
	}
	d := min
	for i := uint32(0); i < attemptCount; i++ {
		d *= 2
		if d >= max {
			return max
		}
	}
	return d
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
