# Data Model: Coordinated Table Workers

**Feature**: 004-coordinated-table-workers
**Date**: 2026-03-29

## Entities

### Task (table: `coordinated_tasks`)

Represents a unit of work inserted by the producer and processed by consumer workers.

| Field         | Type      | Nullable | Description                                                    |
|---------------|-----------|----------|----------------------------------------------------------------|
| id            | UUID      | No       | Unique task identifier (primary key)                           |
| hash          | Int64     | No       | murmur3 hash of task id, used for partition routing            |
| partition_id  | Uint16    | No       | Logical partition = hash % 256 (denormalized for query speed)  |
| priority      | Uint8     | No       | 0-255, higher = more urgent. Default: 128                      |
| status        | String    | No       | One of: "pending", "locked", "completed"                       |
| payload       | String    | No       | Task payload (opaque to workers in this example)               |
| lock_value    | String    | Yes      | Random UUID set when worker locks the task                     |
| locked_until  | Timestamp | Yes      | Lock expiry time; task reclaimable after this                  |
| scheduled_at  | Timestamp | Yes      | If set, task not eligible until this time (postpone)           |
| created_at    | Timestamp | No       | When the producer inserted the task                            |
| done_at       | Timestamp | Yes      | When the worker completed the task                             |

**Primary Key**: `(partition_id, priority, id)` — clustered for efficient per-partition priority-ordered scans.

**Indexes**:

- Primary key covers the main query pattern: fetch highest-priority eligible tasks within a partition.

### Coordination Node (path: configurable, e.g., `/local/04_coordinated_table`)

Not a table — a YDB coordination node hosting semaphores.

**Semaphores**:

- `partition-0` through `partition-255`: Exclusive ephemeral semaphores, one per logical partition. Acquired by workers for ownership.
- `worker-registry`: Shared semaphore with limit=MaxUint64. Each worker acquires count=1 and watches owners for membership change detection.

### Worker (runtime entity, not persisted)

In-memory state per worker process:

| Field              | Description                                           |
|--------------------|-------------------------------------------------------|
| worker_id          | Random UUID assigned at startup                       |
| session            | YDB coordination session                              |
| owned_partitions   | Set of partition IDs currently owned via leases       |
| target_capacity    | ceil(256 / active_worker_count)                       |
| active_worker_count| Derived from worker-registry semaphore owner count    |

## State Transitions

### Task Lifecycle

```text
                  ┌──────────────────────┐
                  │                      │
                  ▼                      │
 [Producer] ──► PENDING ──► LOCKED ──► COMPLETED
                  ▲            │
                  │            │  (locked_until expired)
                  └────────────┘
```

1. **PENDING → LOCKED**: Worker selects highest-priority eligible task in owned partition, sets `lock_value` (random UUID) and `locked_until` (now + lock_duration) atomically.
2. **LOCKED → COMPLETED**: Worker finishes 100ms simulated work, sets `status = "completed"` and `done_at = now`.
3. **LOCKED → PENDING** (implicit): `locked_until` expires — task becomes reclaimable. Next worker poll treats it as eligible and re-locks it (new lock_value overwrites old).

### Eligibility Rules

A task is **eligible** for processing if ALL of:

- `partition_id` is owned by the worker
- `status = "pending"` OR (`status = "locked"` AND `locked_until < now`)
- `scheduled_at IS NULL` OR `scheduled_at <= now`

Eligible tasks are ordered by `priority DESC` (highest first).

## Relationships

```text
Worker ──owns──► Partition (via coordination semaphore lease)
Partition ──contains──► Tasks (via partition_id = hash % 256)
Worker ──processes──► Tasks (within owned partitions, by priority)
```

## Data Volume Assumptions

- 256 logical partitions, 8 workers (configurable)
- Producer inserts continuously; task table grows unboundedly during demo
- Completed tasks remain in table (no TTL/cleanup in this example)
- Lock duration: configurable, default ~5s (enough for 100ms work + margin)
