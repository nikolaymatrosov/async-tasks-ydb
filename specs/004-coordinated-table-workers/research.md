# Research: Coordinated Table Workers

**Feature**: 004-coordinated-table-workers
**Date**: 2026-03-29

## R1: YDB Coordination Semaphore API for Partition Ownership

**Decision**: Use exclusive ephemeral semaphores (`coordination.Exclusive` + `options.WithEphemeral(true)`) — one per logical partition (256 total).

**Rationale**: Ephemeral semaphores auto-release when a session dies, which handles the worker-crash rebalancing scenario without manual cleanup. The lock/workers examples in `docs/coordination/` validate this pattern with the SDK v3.127.0.

**Alternatives considered**:

- Persistent semaphores with attached data: More complex, requires manual cleanup on session loss. Useful if partition metadata needs to survive restarts, but not needed here.
- Shared semaphores with count: Would allow multiple workers per partition, breaking the exclusive-ownership model.

**Key API surface** (from `docs/coordination/lock/main.go` and `docs/coordination/workers/main.go`):

- `db.Coordination().CreateNode(ctx, path, coordination.NodeConfig{...})` — create coordination node
- `db.Coordination().Session(ctx, path)` — open session
- `session.AcquireSemaphore(ctx, name, coordination.Exclusive, options.WithEphemeral(true))` — acquire partition
- `lease.Context()` — lifetime tied to lease ownership
- `lease.Release()` — explicit release for rebalancing
- `session.Context().Done()` — detect session loss

## R2: Partition Rebalancing Strategy

**Decision**: Greedy acquisition with local capacity limiting using `golang.org/x/sync/semaphore.Weighted` (already an indirect dependency via `docs/coordination/workers`).

**Rationale**: The workers example (`docs/coordination/workers/main.go`) demonstrates this exact pattern — acquire all semaphores concurrently, use `TryAcquire` on a local weighted semaphore to cap at target capacity, release excess. This is proven to work with the SDK.

**Rebalancing flow**:

1. Worker starts, calculates target = `ceil(256 / worker_count)`.
2. Worker launches 256 goroutines, each trying `AcquireSemaphore` for one partition.
3. On each successful acquire, check local capacity via `sem.TryAcquire(1)`:
   - If capacity available: accept lease, start processing partition.
   - If at capacity: release lease immediately (let another worker grab it).
4. On session loss or membership change: cancel all acquires, recalculate target, restart.

**Membership detection**: Use a dedicated "registry" semaphore where all workers acquire with count=1 and `DescribeSemaphore` with `WatchOwners=true` to detect membership changes.

**Alternatives considered**:

- Central coordinator (leader election + assignment): Adds complexity, single point of failure. Rejected for an example.
- Consistent hashing ring: Elegant but requires custom implementation not demonstrated by SDK. Overkill for 256 partitions.
- Static assignment (partition_id % worker_count): Simple but doesn't handle dynamic join/leave.

## R3: Task Table Schema and Locking Pattern

**Decision**: Use optimistic locking via compare-and-swap on `lock_value` column within a YDB transaction.

**Rationale**: YDB supports serializable transactions on tables. The worker selects the highest-priority eligible task (status=pending OR status=locked with expired locked_until), then updates it with a new random lock_value and future locked_until in a single transaction. The random lock_value serves as a CAS guard — if two workers race, only the transaction that sees the expected prior lock_value succeeds.

**Query pattern**:

1. SELECT eligible task: `WHERE partition_id = ? AND (status = 'pending' OR (status = 'locked' AND locked_until < CurrentUtcTimestamp())) AND (scheduled_at IS NULL OR scheduled_at <= CurrentUtcTimestamp()) ORDER BY priority DESC LIMIT 1`
2. UPDATE in same tx: set `status = 'locked'`, `lock_value = <random UUID>`, `locked_until = <now + lock_duration>`
3. After 100ms work: UPDATE `status = 'completed'`, `done_at = <now>`

**Alternatives considered**:

- Pessimistic row locking: YDB doesn't support `SELECT ... FOR UPDATE` in the traditional sense. Serializable transactions achieve the same goal.
- Separate lock table: Adds join complexity, no benefit over inline lock columns.

## R4: Hash Routing with murmur3

**Decision**: Use `murmur3.Sum32([]byte(task_id))` to compute hash, store as int64, partition = `hash % 256`.

**Rationale**: Constitution requires murmur3 for partition routing. The 32-bit variant fits in int64 and provides good distribution across 256 buckets. Already used in 03_topic for similar routing.

**Alternatives considered**:

- FNV hash: Explicitly prohibited by constitution.
- Random assignment: Would not allow deterministic partition lookup by task ID.

## R5: Multi-file Layout

**Decision**: Split into main.go, producer.go, worker.go, rebalancer.go, display.go — all `package main`.

**Rationale**: 03_topic established the precedent with 6 source files (main.go, producer.go, consumer.go, message.go, display.go, utils.go). The combined code for this feature is estimated at ~800 lines across 4 concerns. Single main.go would harm readability, which is the primary goal for a learning example.

**Alternatives considered**:

- Single main.go: Constitution principle I requires this, but 03_topic already deviates. A single 800+ line file would be counterproductive for a learning resource.
- Sub-packages: Prohibited by constitution. Would also complicate `go run`.

## R6: New Dependencies Assessment

**Decision**: No new direct dependencies needed.

**Rationale**: All required functionality is covered by existing dependencies:

- `ydb-go-sdk/v3` — coordination API, table API
- `murmur3` — hash routing
- `uuid` — task IDs, lock values
- `ydb-go-yc` — authentication
- `golang.org/x/sync/semaphore` — already an indirect dependency (used in docs/coordination/workers)

Note: `golang.org/x/sync/semaphore` is used for local capacity limiting. It's already in the module graph as an indirect dependency. If it needs to be promoted to a direct dependency, this is justified as it's a standard Go extended library with no additional supply chain risk.

## R7: Exponential Backoff for Empty Partition Polling

**Decision**: Start at 50ms, double on each empty poll, cap at 5s. Reset to 50ms when a task is found.

**Rationale**: 50ms initial delay keeps latency low for bursty workloads. 5s cap prevents excessive polling on truly idle partitions. Simple implementation with `time.Duration` arithmetic — no external backoff library needed.

**Alternatives considered**:

- Fixed interval (500ms): Too slow for high-priority tasks, too fast for idle partitions.
- Jittered backoff: Adds complexity; not needed since partition ownership is exclusive (no thundering herd).
