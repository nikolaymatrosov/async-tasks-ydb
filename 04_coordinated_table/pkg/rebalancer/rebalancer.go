package rebalancer

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/coordination"
	"github.com/ydb-platform/ydb-go-sdk/v3/coordination/options"
	xsync "golang.org/x/sync/semaphore"
)

const registrySemaphore = "worker-registry"

// PartitionEvent is sent on the partition channel when a partition is gained or lost.
type PartitionEvent struct {
	PartitionID int
	Lease       coordination.Lease // non-nil = acquired; nil = released
}

// Rebalancer manages partition semaphore acquisition and dynamic rebalancing.
type Rebalancer struct {
	db               *ydb.Driver
	coordinationPath string
	workerID         string
	partitionCount   int

	mu             sync.Mutex
	session        coordination.Session
	leases         map[int]coordination.Lease
	targetCapacity int64

	partitionCh chan PartitionEvent
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

func NewRebalancer(db *ydb.Driver, coordinationPath, workerID string, partitionCount int) *Rebalancer {
	return &Rebalancer{
		db:               db,
		coordinationPath: coordinationPath,
		workerID:         workerID,
		partitionCount:   partitionCount,
		leases:           make(map[int]coordination.Lease),
		partitionCh:      make(chan PartitionEvent, partitionCount*2),
		stopCh:           make(chan struct{}),
	}
}

// Start opens a coordination session, acquires the worker-registry semaphore,
// and begins greedy partition acquisition. Returns a channel of partition events.
func (r *Rebalancer) Start(ctx context.Context) (<-chan PartitionEvent, error) {
	session, err := r.db.Coordination().Session(ctx, r.coordinationPath,
		options.WithDescription(fmt.Sprintf("worker-%s", r.workerID)),
	)
	if err != nil {
		return nil, fmt.Errorf("open coordination session: %w", err)
	}

	r.mu.Lock()
	r.session = session
	r.mu.Unlock()

	registryLease, err := session.AcquireSemaphore(ctx, registrySemaphore, coordination.Shared,
		options.WithEphemeral(true),
		options.WithAcquireData([]byte(r.workerID)),
	)
	if err != nil {
		_ = session.Close(context.Background())
		return nil, fmt.Errorf("acquire worker-registry: %w", err)
	}
	slog.Info("worker registered", "worker_id", r.workerID)

	workerCount := r.describeWorkerCount(ctx, session)
	r.mu.Lock()
	r.targetCapacity = ceilDiv(int64(r.partitionCount), int64(workerCount))
	r.mu.Unlock()
	slog.Info("initial target capacity", "worker_id", r.workerID, "workers", workerCount, "target", r.targetCapacity)

	r.wg.Add(1)
	go r.acquireLoop(ctx, session, registryLease)

	return r.partitionCh, nil
}

func (r *Rebalancer) acquireLoop(ctx context.Context, session coordination.Session, registryLease coordination.Lease) {
	defer r.wg.Done()
	defer func() { _ = registryLease.Release() }()
	defer func() { _ = session.Close(context.Background()) }()

	for {
		r.mu.Lock()
		target := r.targetCapacity
		r.mu.Unlock()

		localSem := xsync.NewWeighted(target)
		acquireCtx, cancelAcquire := context.WithCancel(ctx)
		var acquireWg sync.WaitGroup

		for i := 0; i < r.partitionCount; i++ {
			r.mu.Lock()
			_, owned := r.leases[i]
			r.mu.Unlock()
			if owned {
				localSem.TryAcquire(1) //nolint:errcheck
				continue
			}

			acquireWg.Add(1)
			go r.tryAcquirePartition(acquireCtx, &acquireWg, session, i, localSem)
		}

		watchCtx, cancelWatch := context.WithCancel(ctx)
		memberChangeCh := make(chan int, 4)
		go r.watchMembership(watchCtx, session, memberChangeCh)

		select {
		case <-ctx.Done():
			cancelAcquire()
			cancelWatch()
			acquireWg.Wait()
			r.releaseAll()
			close(r.partitionCh)
			return

		case <-session.Context().Done():
			slog.Warn("coordination session lost, restarting", "worker_id", r.workerID)
			cancelAcquire()
			cancelWatch()
			acquireWg.Wait()
			r.releaseAllLocally()

			newSession, err := r.db.Coordination().Session(ctx, r.coordinationPath,
				options.WithDescription(fmt.Sprintf("worker-%s", r.workerID)),
			)
			if err != nil {
				if ctx.Err() != nil {
					close(r.partitionCh)
					return
				}
				slog.Error("failed to reopen coordination session", "err", err)
				close(r.partitionCh)
				return
			}
			r.mu.Lock()
			r.session = newSession
			r.mu.Unlock()
			session = newSession
			registryLease, err = newSession.AcquireSemaphore(ctx, registrySemaphore, coordination.Shared,
				options.WithEphemeral(true),
				options.WithAcquireData([]byte(r.workerID)),
			)
			if err != nil {
				if ctx.Err() != nil {
					close(r.partitionCh)
					return
				}
				slog.Error("failed to re-acquire worker-registry", "err", err)
				close(r.partitionCh)
				return
			}
			continue

		case newCount := <-memberChangeCh:
			cancelAcquire()
			cancelWatch()
			acquireWg.Wait()

			r.mu.Lock()
			oldTarget := r.targetCapacity
			r.targetCapacity = ceilDiv(int64(r.partitionCount), int64(newCount))
			newTarget := r.targetCapacity
			r.mu.Unlock()

			reason := "worker_joined"
			if int64(newCount)*oldTarget > int64(r.partitionCount) {
				reason = "worker_left"
			}
			slog.Info("rebalancing",
				"worker_id", r.workerID,
				"old_count", oldTarget,
				"new_count", newTarget,
				"reason", reason,
				"active_workers", newCount,
			)
			r.releaseExcess(newTarget)
		}

		select {
		case <-r.stopCh:
			r.releaseAll()
			close(r.partitionCh)
			return
		default:
		}
	}
}

func (r *Rebalancer) tryAcquirePartition(
	ctx context.Context,
	wg *sync.WaitGroup,
	session coordination.Session,
	partitionID int,
	localSem *xsync.Weighted,
) {
	defer wg.Done()

	semName := fmt.Sprintf("partition-%d", partitionID)
	lease, err := session.AcquireSemaphore(ctx, semName, coordination.Exclusive,
		options.WithEphemeral(true),
	)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.Debug("acquire partition failed", "partition_id", partitionID, "err", err)
		return
	}

	if !localSem.TryAcquire(1) {
		_ = lease.Release()
		return
	}

	r.mu.Lock()
	r.leases[partitionID] = lease
	r.mu.Unlock()

	slog.Info("partition acquired", "worker_id", r.workerID, "partition_id", partitionID)
	r.partitionCh <- PartitionEvent{PartitionID: partitionID, Lease: lease}
}

func (r *Rebalancer) watchMembership(ctx context.Context, session coordination.Session, ch chan<- int) {
	var lastCount int
	for {
		desc, err := session.DescribeSemaphore(ctx, registrySemaphore,
			options.WithDescribeOwners(true),
		)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		count := len(desc.Owners)
		if count != lastCount && lastCount != 0 {
			select {
			case ch <- count:
			default:
			}
		}
		lastCount = count

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (r *Rebalancer) describeWorkerCount(ctx context.Context, session coordination.Session) int {
	desc, err := session.DescribeSemaphore(ctx, registrySemaphore,
		options.WithDescribeOwners(true),
	)
	if err != nil || len(desc.Owners) == 0 {
		return 1
	}
	return len(desc.Owners)
}

func (r *Rebalancer) releaseExcess(newTarget int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current := int64(len(r.leases))
	excess := current - newTarget
	if excess <= 0 {
		return
	}

	released := int64(0)
	for partitionID, lease := range r.leases {
		if released >= excess {
			break
		}
		if err := lease.Release(); err != nil {
			slog.Warn("lease release failed", "partition_id", partitionID, "err", err)
		}
		delete(r.leases, partitionID)
		r.partitionCh <- PartitionEvent{PartitionID: partitionID, Lease: nil}
		slog.Info("partition released (rebalance)", "worker_id", r.workerID, "partition_id", partitionID)
		released++
	}
}

func (r *Rebalancer) releaseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for partitionID, lease := range r.leases {
		if err := lease.Release(); err != nil {
			slog.Warn("lease release failed on shutdown", "partition_id", partitionID, "err", err)
		}
		r.partitionCh <- PartitionEvent{PartitionID: partitionID, Lease: nil}
		delete(r.leases, partitionID)
	}
}

func (r *Rebalancer) releaseAllLocally() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for partitionID := range r.leases {
		r.partitionCh <- PartitionEvent{PartitionID: partitionID, Lease: nil}
		delete(r.leases, partitionID)
	}
}

// Stop signals the rebalancer to shut down and waits for it to finish.
func (r *Rebalancer) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

func ceilDiv(a, b int64) int64 {
	if b == 0 {
		return int64(math.MaxInt64)
	}
	return (a + b - 1) / b
}
