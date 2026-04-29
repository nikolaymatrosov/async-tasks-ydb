# Phase 1 Data Model: Per-Entity Ordered Task Delivery

**Feature**: 017-entity-task-ordering
**Date**: 2026-04-29 (revised after `/speckit.clarify` session same day)

## 1. New table: `ordered_tasks`

Created fresh by goose migration `20260429000007_create_ordered_tasks.sql`. Forked from
`coordinated_tasks` with `priority` and `hash` removed and the per-entity ordering columns added.

| Column | YDB type | Nullable | Notes |
| --- | --- | --- | --- |
| `id` | `Utf8` | No | UUIDv4 of the task |
| `partition_id` | `Uint16` | No | `uint16(murmur3.Sum32([]byte(entity_id)) % partitions)` |
| `entity_id` | `Utf8` | No | Domain identifier supplied by the producer (FR-001) |
| `entity_seq` | `Uint64` | No | Producer-generated synthetic monotonic ordinal (FR-002); strictly increasing per producer process; sparse per entity |
| `status` | `Utf8` | No | Vocabulary: `pending`, `locked`, `completed`, `failed`, `skipped` (§1.2) |
| `payload` | `Utf8` | No | JSON; producer writes `{"url": "..."}` exactly as `04_coordinated_table` does |
| `lock_value` | `Utf8` | Yes | Worker UUID held while `status='locked'` |
| `locked_until` | `Timestamp` | Yes | Lease expiry while `status='locked'` |
| `scheduled_at` | `Timestamp` | Yes | Backoff retry deadline (set by `MarkFailedWithBackoff`) |
| `attempt_count` | `Uint32` | No | Number of failed processing attempts; drives terminal-failure decision (FR-008) |
| `last_error` | `Utf8` | Yes | Diagnostic from the last `MarkFailedWithBackoff` / `MarkTerminallyFailed` call |
| `resolved_by` | `Utf8` | Yes | Operator identity recorded at FR-009 resolution |
| `resolved_at` | `Timestamp` | Yes | Time of FR-009 resolution |
| `created_at` | `Timestamp` | No | Producer's UPSERT timestamp |
| `done_at` | `Timestamp` | Yes | Set on terminal `completed`/`failed`/`skipped` |

### 1.1 Primary key and index

- `PRIMARY KEY (partition_id, id)` — `priority` is gone; `id` (UUIDv4) is unique enough to
  disambiguate within a partition.
- `INDEX idx_partition_entity_seq GLOBAL ON (partition_id, entity_id, entity_seq) COVER (id,
  payload, status, scheduled_at, locked_until, attempt_count)` — supports the worker's
  head-of-entity scan (research §3 / §4).

### 1.2 Status vocabulary

| Status | Eligibility-scan-visible? | Meaning |
| --- | --- | --- |
| `pending` | Yes | Newly inserted or coming back from backoff |
| `locked` | Yes | A worker holds the lease (`lock_value`, `locked_until`) |
| `completed` | No | Terminal success (FR-007) |
| `failed` | No | Terminal failure — retries exhausted (FR-008); blocks the entity until operator action |
| `skipped` | No | Operator-resolved over a `failed` head (FR-009) |

Eligibility-scan predicate: `status IN ('pending', 'locked')`. Terminal states are invisible, so
the smallest non-terminal `entity_seq` for a given `entity_id` is, by construction, the head.

### 1.3 State transitions

```text
                       producer UPSERT
                              │
                              ▼
                          ┌────────┐  worker ClaimTask CAS
                          │pending │ ─────────────────────► ┌────────┐
                          └────────┘                        │ locked │
                              ▲                             └───┬────┘
              MarkFailedWithBackoff (scheduled_at = retry_at,   │
              attempt_count++; status back to pending)          │
                              │                                 │
                              └─────────────────────────────────┤
                                                                │
                                                  ┌─────────────┼─────────────┐
                                                  ▼             ▼             ▼
                                              completed       failed       skipped
                                              (success)   (max attempts) (operator)
                                                  │             │             │
                                                  └─────────────┴─────────────┘
                                                          terminal
```

Invariants:

- Per-entity ordering: at any instant, for any `entity_id`, **at most one row** has `status IN
  ('pending', 'locked')` *and* is the smallest `entity_seq` for that entity. (Smaller seqs are
  terminal; larger seqs are gated by the head.)
- All transitions out of `locked` (to `pending`/`completed`/`failed`) are conditioned on the
  current `lock_value` matching the caller's, making them idempotent under at-least-once
  delivery (FR-011).

### 1.4 Validation rules

- `entity_id` is opaque, non-empty, ≤ 256 bytes — enforced by the producer.
- `entity_seq ≥ 1` — the producer's `UnixNano()*1024 + atomic` formula is always positive.
- `(partition_id, entity_id, entity_seq)` is unique — enforced by the producer's globally
  strictly-increasing seq generator (research §2). Single-producer assumption applies.
- Sequences are **strictly monotonic but sparse** per entity: for any two events `e1`, `e2` of
  the same entity with `e1` written before `e2`, `e1.entity_seq < e2.entity_seq` (no
  contiguity guarantee).
- `attempt_count ≤ max_attempts` while `status ∈ {pending, locked}`; equality with
  `max_attempts` triggers the next failure to write `status='failed'` instead of recycling to
  `pending`.

## 2. Go domain types

```go
// pkg/taskworker/repository.go

type Candidate struct {
    ID           string
    EntityID     string
    EntitySeq    uint64
    Payload      string
    Status       string      // "pending" | "locked"
    ScheduledAt  *time.Time  // future => entity in backoff
    LockedUntil  *time.Time  // for reclaim decisions
    AttemptCount uint32
}

type ClaimedTask struct {
    ID           string
    PartitionID  uint16
    EntityID     string
    EntitySeq    uint64
    Payload      string
    LockValue    string
    LockedUntil  time.Time
    AttemptCount uint32
}

type TaskRepository interface {
    FetchEligibleHeads(ctx context.Context, partitionID uint16, k int) ([]Candidate, error)
    ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error)
    MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error
    MarkFailedWithBackoff(ctx context.Context, task ClaimedTask, retryAt time.Time, lastError string) error
    MarkTerminallyFailed(ctx context.Context, task ClaimedTask, failedAt time.Time, lastError string) error
}
```

`priority` is removed throughout — neither `Candidate` nor `ClaimedTask` carries it.

## 3. Producer model (single-instance)

`pkg/taskproducer/producer.go`:

- One process at a time (Clarifications). Process-wide strictly-increasing seq generator:

  ```go
  var seqCounter atomic.Uint64
  func nextSeq() uint64 {
      return uint64(time.Now().UnixNano())*1024 + seqCounter.Add(1)
  }
  ```

- `taskRow` struct: `id, partitionID, entityID, entitySeq, payload, createdAt, scheduledAt`.
  No `priority`. No `hash`.
- Partition: `partitionID = uint16(uint64(murmur3.Sum32([]byte(entityID))) % uint64(partitions))`.
- Entity pool: `--entities N` CLI flag synthesises `entity-0000000` … `entity-0000(N-1)` and
  picks uniformly per task.
- Batch upsert via `AS_TABLE($records)` exactly as in `04_coordinated_table` — but writing the
  new schema (no priority column).

## 4. Target server in-memory model (`06_target_server`)

```go
type entityState struct {
    LastAcceptedSeq uint64
    Accepted        uint64
    LastAcceptedAt  time.Time
}

var state struct {
    mu         sync.Mutex      // sharded; see research §10
    perEntity  map[string]*entityState
    violations atomic.Uint64
    accepted   atomic.Uint64
    fault429   atomic.Uint64
    fault5xx   atomic.Uint64
}
```

Bucketed metric label: `bucket = murmur3.Sum32([]byte(entity_id)) % 64` — keeps Prometheus
cardinality bounded.

## 5. Migration file

`migrations/20260429000007_create_ordered_tasks.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE ordered_tasks (
    id            Utf8         NOT NULL,
    partition_id  Uint16       NOT NULL,
    entity_id     Utf8         NOT NULL,
    entity_seq    Uint64       NOT NULL,
    status        Utf8         NOT NULL,
    payload       Utf8         NOT NULL,
    lock_value    Utf8,
    locked_until  Timestamp,
    scheduled_at  Timestamp,
    attempt_count Uint32       NOT NULL,
    last_error    Utf8,
    resolved_by   Utf8,
    resolved_at   Timestamp,
    created_at    Timestamp    NOT NULL,
    done_at       Timestamp,
    PRIMARY KEY (partition_id, id),
    INDEX idx_partition_entity_seq GLOBAL ON (partition_id, entity_id, entity_seq)
      COVER (id, payload, status, scheduled_at, locked_until, attempt_count)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ordered_tasks;
-- +goose StatementEnd
```
