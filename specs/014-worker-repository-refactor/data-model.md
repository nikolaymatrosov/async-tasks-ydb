# Phase 1 Data Model: Worker Repository Refactor

**Branch**: `014-worker-repository-refactor` | **Date**: 2026-04-25

This refactor introduces no new persisted entities — the `coordinated_tasks` table schema, columns, types, status vocabulary, and partition-key shape are unchanged (FR-012). What follows are the **in-memory value types** that cross the new repository boundary, plus a recap of the underlying table for readers who need to trace a domain field back to a column.

## In-memory value types (new)

### `Candidate`

A task fetched from the table but **not yet claimed** by the calling worker. Returned by `FetchEligibleCandidate`. Fed back to `ClaimTask` to identify the row to lock.

| Field    | Type     | Source column            | Notes                                                                |
| -------- | -------- | ------------------------ | -------------------------------------------------------------------- |
| ID       | string   | `id` (Utf8)              | Task UUID. Part of PK alongside `partition_id` and `priority`.        |
| Priority | uint8    | `priority` (Uint8)       | 0–255. Selection orders by `priority DESC`. Part of PK.              |
| Payload  | string   | `payload` (Utf8)         | Opaque to the worker; passed to the user-supplied `ProcessTask`.      |

**Validation**: none at the type level — the repository asserts uniqueness via the conditional claim (the row may or may not still be claimable when `ClaimTask` runs). The Candidate is never persisted; it is a transient handle.

**Why no `partition_id` field**: the caller already knows the partition (it passed `partitionID` to `FetchEligibleCandidate`). Adding `PartitionID` to `Candidate` would let the worker accidentally pass a candidate to `ClaimTask` with a different partition — this footgun is removed by making partition a separate parameter on `ClaimTask`.

### `ClaimedTask`

A task that this worker has successfully transitioned to `status = 'locked'`. Returned by `ClaimTask` on success. Fed to the user-supplied processor and to `MarkCompleted`.

| Field        | Type      | Source column                | Notes                                                                                |
| ------------ | --------- | ---------------------------- | ------------------------------------------------------------------------------------ |
| ID           | string    | `id` (Utf8)                  | Echoed from `Candidate.ID`.                                                          |
| PartitionID  | uint16    | `partition_id` (Uint16)      | Echoed from the `partitionID` argument.                                              |
| Priority     | uint8    | `priority` (Uint8)           | Echoed from `Candidate.Priority`.                                                    |
| Payload      | string    | `payload` (Utf8)             | Echoed from `Candidate.Payload`.                                                     |
| LockValue    | string    | `lock_value` (Utf8)          | Echoed from the `lockValue` argument; identifies *this* worker's claim.              |
| LockedUntil  | time.Time | `locked_until` (Timestamp)   | Echoed from the `lockedUntil` argument; UTC absolute deadline.                       |

**Validation**: none at the type level. `LockedUntil` is supplied by the caller and is expected to be a UTC timestamp in the future at the moment of the claim; the repository does not re-validate it.

**State transitions** (in the table, observed by this type):

```text
                            FetchEligibleCandidate
                                     │
                                     ▼
   pending / (locked & expired)  ──── ClaimTask success ────▶  locked
                                     │
                              [worker invokes ProcessTask]
                                     │
                                     ▼
                                 MarkCompleted ────────────▶  completed
```

A `ClaimTask` no-op (lost race) returns `(nil, nil)` and produces no `ClaimedTask`; the row stays in whatever state the winning owner left it.

A processor failure (FR-007 / spec Edge Cases) does **not** call `MarkCompleted`; the row's `locked_until` expires naturally and a future `FetchEligibleCandidate` cycle reclaims it. No `ClaimedTask` field needs to encode this — the *absence* of the `MarkCompleted` call is the encoding.

### `TaskRepository` (interface)

The named storage abstraction. See `contracts/task_repository.md` for the full per-method contract; the data-model entry exists so a reader of this document sees the surface in one place.

```go
type TaskRepository interface {
    FetchEligibleCandidate(ctx context.Context, partitionID uint16) (*Candidate, error)
    ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error)
    MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error
}
```

**Concrete implementations**:

- `ydbTaskRepository` (production) — wraps `*ydb.Driver`; constructed by `NewYDBRepository(db *ydb.Driver) TaskRepository` from `repository_ydb.go`. Lives in `cmd/worker/main.go`'s composition root.
- `fakeRepository` (test-only) — defined inside `worker_test.go`; replays a scripted slice of outcomes per call. Not exported.

## Eligibility predicate (table-level invariant the repository owns)

A row is eligible to be returned by `FetchEligibleCandidate(ctx, partitionID)` iff **all** of:

1. `partition_id = partitionID`
2. `status = 'pending'` **OR** (`status = 'locked'` AND `locked_until < CurrentUtcTimestamp()`)
3. `scheduled_at IS NULL` **OR** `scheduled_at <= CurrentUtcTimestamp()`

The highest-priority such row is returned (`ORDER BY priority DESC LIMIT 1`). This predicate **MUST** live exclusively in `repository_ydb.go` — SC-005 verifies that modifying the predicate (e.g., to add a `failed` status or a new exclusion) requires editing exactly one file inside the worker package, with no change to `worker.go`.

## Underlying table (recap, unchanged)

```text
coordinated_tasks (existing)
├── id            Utf8         (PK part 3)
├── hash          Int64
├── partition_id  Uint16       (PK part 1)
├── priority      Uint8        (PK part 2)
├── status        Utf8         {'pending','locked','completed'}
├── payload       Utf8
├── lock_value    Utf8         (set by ClaimTask)
├── locked_until  Timestamp    (set by ClaimTask; UTC)
├── created_at    Timestamp    (set by producer)
├── scheduled_at  Timestamp?   (optional; set by producer)
└── done_at       Timestamp    (set by MarkCompleted)
```

The PK ordering (`partition_id`, `priority`, `id`) is what the conditional UPDATE in `ClaimTask` keys on, and what `MarkCompleted` keys on. The repository **must** issue point updates by full PK (not by `id` alone) — this is part of the preserved two-phase strategy and is enforced by the contract in `contracts/task_repository.md`.

## What this refactor does **not** add

- No `Failed` status, no dead-letter table, no retry counter — explicitly out of scope (FR-007 and spec Edge Cases case 2).
- No new `TaskHistory` or `TaskAudit` entity — logging stays at the worker layer (FR-008).
- No generic `Repository[T]` abstraction — the surface is exactly three methods scoped to this one table.
