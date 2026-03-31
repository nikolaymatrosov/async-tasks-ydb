# Feature Specification: Coordinated Table Workers

**Feature Branch**: `004-coordinated-table-workers`
**Created**: 2026-03-29
**Status**: Draft
**Input**: User description: "Add new 04_coordinated_table example with producer-filled table, coordination-node-based consumer workers, hash-partitioned task locking, and dynamic partition rebalancing across workers."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Producer fills tasks table (Priority: P1)

A producer continuously inserts task rows into a YDB table. Each row contains a hash column (int64) that determines which logical partition (0-255) the task belongs to. The producer runs independently of consumers.

**Why this priority**: Without tasks in the table, consumers have nothing to process. The producer is the data source for the entire example.

**Independent Test**: Can be tested by running the producer alone and verifying rows appear in the tasks table with valid hash values distributed across partitions.

**Acceptance Scenarios**:

1. **Given** an empty tasks table, **When** the producer starts, **Then** it inserts rows with a hash column whose remainder modulo 256 distributes tasks across all 256 logical partitions.
2. **Given** a running producer, **When** it inserts a row, **Then** the row has status "pending", a priority value (0-255), no lock value, and no locked_until timestamp.

---

### User Story 2 - Worker acquires partitions and processes tasks (Priority: P1)

A consumer worker starts, connects to the YDB coordination node, acquires ownership of one or more logical partitions via semaphores, and begins processing tasks belonging to those partitions. When processing a task, the worker marks it with a random lock value and sets a locked_until timestamp, then simulates work by sleeping 100ms in a goroutine.

**Why this priority**: This is the core functionality of the example — demonstrating coordinated task processing with partition-based work distribution.

**Independent Test**: Can be tested by starting the producer and a single worker, then verifying the worker acquires partitions, locks tasks, and marks them as processed after the simulated work completes.

**Acceptance Scenarios**:

1. **Given** a running coordination node and tasks in the table, **When** a single worker starts, **Then** it acquires all 256 partitions and begins processing tasks from each.
2. **Given** a worker owns partition P with multiple pending tasks, **When** it selects the next task, **Then** it picks the highest-priority task first (highest integer value), sets a random lock value, and updates locked_until to a future timestamp before simulating work.
3. **Given** a worker is processing a task, **When** the 100ms simulated work completes, **Then** the task is marked as completed.

---

### User Story 3 - Dynamic partition rebalancing across workers (Priority: P1)

When multiple workers are running, the 256 logical partitions are distributed among them. When a new worker joins or an existing worker dies, partitions are automatically redistributed so that each worker handles a roughly equal share (e.g., 8 workers each handle ~32 partitions).

**Why this priority**: Rebalancing is the key distributed systems concept this example demonstrates. Without it, the example is just a single-worker task processor.

**Independent Test**: Can be tested by starting 2 workers and verifying each handles ~128 partitions, then starting a 3rd worker and verifying partitions redistribute to ~85 each.

**Acceptance Scenarios**:

1. **Given** 8 running workers, **When** all have joined the coordination node, **Then** each worker owns approximately 32 partitions (256 / 8).
2. **Given** 8 running workers each owning 32 partitions, **When** one worker dies, **Then** its 32 partitions are redistributed among the remaining 7 workers.
3. **Given** 7 running workers, **When** a new (8th) worker starts, **Then** partitions rebalance so each worker again owns approximately 32 partitions.

---

### User Story 4 - Priority-based task processing and postponed tasks (Priority: P2)

The producer creates tasks with varying priorities (0-255) and optionally sets a scheduled_at timestamp for future execution. Workers within each partition always pick the highest-priority eligible task first. Tasks with scheduled_at in the future are skipped until that time arrives.

**Why this priority**: Priority and postpone add realistic scheduling semantics on top of the core partition-based processing, making the example useful for real-world task queue patterns.

**Independent Test**: Can be tested by inserting tasks with different priorities and scheduled_at values into a single partition, then verifying the worker processes them in priority order and skips postponed tasks until they become eligible.

**Acceptance Scenarios**:

1. **Given** a partition with tasks at priority 200 and priority 50, **When** the worker polls, **Then** it picks the priority-200 task first.
2. **Given** a task with scheduled_at 10 seconds in the future, **When** the worker polls now, **Then** it skips the task. **When** polled after scheduled_at, **Then** it picks up the task.
3. **Given** a partition where all pending tasks are postponed, **When** the worker polls, **Then** it applies exponential backoff until a task becomes eligible.

---

### User Story 5 - Stale lock recovery (Priority: P2)

If a worker crashes while holding a task lock, other workers can reclaim tasks whose locked_until timestamp has expired.

**Why this priority**: This ensures the system is resilient to worker failures and tasks are not permanently stuck.

**Independent Test**: Can be tested by locking a task with a short locked_until, letting it expire, and verifying another worker picks it up.

**Acceptance Scenarios**:

1. **Given** a task locked by a crashed worker with locked_until in the past, **When** another worker scans its partition, **Then** it reclaims the task by setting a new lock value and locked_until.

---

### Edge Cases

- What happens when all workers die simultaneously? Tasks remain in the table with expired locks; when any worker restarts, it picks up all 256 partitions and resumes processing.
- What happens when two workers race to lock the same task? Only one succeeds due to optimistic locking (the lock value acts as a compare-and-swap guard); the other retries with the next available task.
- What happens when the coordination node becomes temporarily unavailable? Workers lose their sessions; they stop processing and attempt to re-establish sessions and re-acquire partitions.
- What happens when the producer inserts tasks faster than workers can process them? Tasks accumulate in the table; workers process them at their own pace. Backpressure is not required for this example.
- What happens when all pending tasks in a partition are postponed (scheduled_at in the future)? The worker skips the partition until at least one task becomes eligible, then resumes on the next poll cycle.
- What happens when low-priority tasks are never processed because high-priority tasks keep arriving? No starvation prevention is required for this example; higher-priority tasks always take precedence.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST provide a producer that inserts task rows into a YDB table, each with an int64 hash column that determines the logical partition (hash % 256) and a priority value (integer 0-255, where higher = more urgent).
- **FR-002**: System MUST create 256 semaphores on a YDB coordination node, one per logical partition (e.g., `partition-0` through `partition-255`).
- **FR-003**: Each consumer worker MUST acquire exclusive ownership of partition semaphores via the coordination node to determine which partitions it processes.
- **FR-004**: When a worker picks up a pending task from an owned partition, it MUST select the highest-priority eligible task first and atomically set a random lock value and update the locked_until timestamp.
- **FR-005**: Workers MUST simulate task processing by sleeping 100ms in a goroutine, then marking the task as completed.
- **FR-006**: When a worker joins or leaves, partitions MUST be redistributed among remaining workers so each handles a roughly equal share.
- **FR-007**: Workers MUST reclaim tasks whose locked_until timestamp has expired (stale lock recovery).
- **FR-008**: The system MUST support 256 logical partitions and 8 concurrent workers as the default configuration.
- **FR-009**: Tasks MUST support an optional scheduled_at timestamp set by the producer at insert time (immutable after insert); workers MUST NOT process tasks whose scheduled_at is in the future ("postponed" tasks become eligible only after that time).
- **FR-010**: Workers MUST order task selection by priority descending (highest first) among eligible tasks within each owned partition.
- **FR-011**: When no eligible tasks are found in a partition, the worker MUST use exponential backoff before re-polling (starting short, increasing up to a configurable cap), resetting the backoff when a task is found.

### Rebalancing Strategy

The partition rebalancing uses a **greedy acquisition with capacity limiting** approach via YDB coordination semaphores:

1. **Each partition is a semaphore**: 256 exclusive ephemeral semaphores (`partition-0` ... `partition-255`) are created on the coordination node.
2. **Workers acquire greedily**: On startup, each worker attempts to acquire all 256 semaphores concurrently. Since semaphores are exclusive, only one worker can hold each.
3. **Capacity-limited**: Each worker has a target capacity of `ceil(256 / active_workers)`. Once a worker reaches its target, it stops acquiring and releases any excess semaphores it holds.
4. **Rebalancing on membership change**: Workers watch the coordination node for session changes (via `DescribeSemaphore` with `WatchOwners`). When the set of active workers changes (join or leave), each worker recalculates its target capacity and releases excess partitions or attempts to acquire newly available ones.
5. **Graceful handoff**: When a worker is shutting down, it releases all its semaphores, making them available for other workers to acquire immediately.

This approach ensures:

- No central coordinator is needed — rebalancing is fully decentralized.
- Partition redistribution happens automatically when workers join or leave.
- The coordination node's semaphore semantics guarantee exclusivity — no two workers process the same partition simultaneously.

### Key Entities

- **Task**: A row in the tasks table representing a unit of work. Key attributes: id, hash (int64), priority (int, 0-255, higher = more urgent), status (pending/locked/completed), lock_value (random string), locked_until (timestamp), scheduled_at (optional timestamp — task is not eligible until this time).
- **Logical Partition**: A virtual partition identified by `hash % 256`. Each partition maps to a coordination semaphore.
- **Worker**: A consumer process that owns a set of logical partitions and processes tasks belonging to those partitions.
- **Coordination Node**: A YDB coordination node hosting the 256 partition semaphores used for distributed ownership.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With 8 workers running, all 256 partitions are assigned and actively processed within 10 seconds of the last worker joining.
- **SC-002**: When a worker dies, its partitions are reassigned to remaining workers within 5 seconds (bounded by coordination node session grace period).
- **SC-003**: No task is processed by more than one worker simultaneously — verified by checking that no two workers hold the same lock value for any task.
- **SC-004**: Tasks with expired locked_until timestamps are reclaimed and reprocessed within one polling cycle of an owning worker.
- **SC-005**: The example runs as a self-contained binary with configurable partition count, worker count, and YDB connection parameters.
- **SC-006**: Given tasks with different priorities in the same partition, the higher-priority task is always processed first.
- **SC-007**: Tasks with scheduled_at in the future are not processed until that time arrives.

## Clarifications

### Session 2026-03-29

- Q: What priority model should tasks use? → A: Arbitrary integer 0-255, higher = more urgent.
- Q: Who sets scheduled_at and when? → A: Producer sets scheduled_at at insert time only; it is immutable after insert.
- Q: How should workers behave when no eligible tasks found? → A: Exponential backoff on repeated empty polls, up to a cap.

## Assumptions

- YDB coordination node is available and supports the semaphore API as documented.
- The tasks table is created as part of the example setup (or via a migration).
- Worker count (8) and partition count (256) are configurable defaults, not hardcoded limits.
- The 100ms sleep is a placeholder for real work; in production, this would be replaced with actual task processing logic.
- Workers poll the tasks table periodically rather than using push-based notifications for new tasks.
