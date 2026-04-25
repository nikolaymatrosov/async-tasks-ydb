# Contract: `TaskRepository` Interface

**Branch**: `014-worker-repository-refactor` | **Date**: 2026-04-25
**Module**: `04_coordinated_table/pkg/taskworker` (file: `repository.go`)

This document is the binding contract for the `TaskRepository` interface. The Worker depends only on this contract; the YDB implementation in `repository_ydb.go` and the test fake in `worker_test.go` MUST both honour it. Changes to method signatures, parameter meanings, or outcome semantics require a spec amendment.

## Surface

```go
package taskworker

import (
    "context"
    "time"
)

type Candidate struct {
    ID       string
    Priority uint8
    Payload  string
}

type ClaimedTask struct {
    ID          string
    PartitionID uint16
    Priority    uint8
    Payload     string
    LockValue   string
    LockedUntil time.Time
}

type TaskRepository interface {
    FetchEligibleCandidate(ctx context.Context, partitionID uint16) (*Candidate, error)
    ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error)
    MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error
}
```

---

## Method 1: `FetchEligibleCandidate`

```go
FetchEligibleCandidate(ctx context.Context, partitionID uint16) (*Candidate, error)
```

### Purpose

Return the highest-priority task in `partitionID` that is currently claimable, **without** taking a write lock. Implements phase 1 of the two-phase locking strategy.

### Inputs

- `ctx`: cancellation signal. The implementation MUST propagate cancellation; cancelled context returns `(nil, ctx.Err())` (or a wrapped form whose `errors.Is(err, context.Canceled)` is true).
- `partitionID`: the logical partition this worker currently owns. Any value (including outside the configured partition range) is valid input — out-of-range partitions simply have zero matching rows and yield the no-op outcome.

### Outcomes

| Outcome | Return | Worker reaction |
| ------- | ------ | --------------- |
| Eligible candidate found | `(&Candidate{ID, Priority, Payload}, nil)` | Proceed to `ClaimTask`. |
| No eligible task in this partition | `(nil, nil)` | Sleep `backoff`, double `backoff` toward `BackoffMax`, repeat. |
| Context cancelled / lease lost | `(nil, error)` where `errors.Is(err, context.Canceled)` is true | Exit the partition loop without recording an error metric. |
| Transient backend error | `(nil, error)` (non-`context.Canceled`) | Increment `Stats.Errors`, log at `warn`, sleep `backoff`, double `backoff`. |

### Eligibility predicate (binding)

A row is returned iff **all** of:

1. `partition_id = partitionID`
2. `status = 'pending'` OR (`status = 'locked'` AND `locked_until < CurrentUtcTimestamp()`)
3. `scheduled_at IS NULL` OR `scheduled_at <= CurrentUtcTimestamp()`

Selection: `ORDER BY priority DESC LIMIT 1`. Implementations MUST NOT add additional filters or change the ordering without a spec amendment.

### Implementation constraints

- MUST use a snapshot read-only transaction (no row locks). The current YDB implementation uses `query.SnapshotReadOnlyTxControl()`; this is preserved verbatim (FR-007).
- MUST NOT log or update metrics (FR-008, FR-009).
- MUST NOT panic on any input.

---

## Method 2: `ClaimTask`

```go
ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error)
```

### Purpose

Atomically transition the row identified by (`partitionID`, `c.Priority`, `c.ID`) from a claimable state to `status = 'locked'` with the supplied `lockValue` and `lockedUntil`. Implements phase 2 of the two-phase locking strategy.

### Inputs

- `ctx`: cancellation signal — propagated as in `FetchEligibleCandidate`.
- `partitionID`: same partition the candidate was fetched from.
- `c`: the candidate returned by a prior `FetchEligibleCandidate`. The repository keys the conditional update on (`partitionID`, `c.Priority`, `c.ID`) — full PK.
- `lockValue`: opaque string identifying *this* worker's claim. Caller-supplied (typically a UUID); MUST be non-empty.
- `lockedUntil`: absolute UTC deadline after which a competing worker may reclaim the row. Caller-supplied; MUST be in the future at call time.

### Outcomes

| Outcome | Return | Worker reaction |
| ------- | ------ | --------------- |
| Row was claimable; transition committed | `(&ClaimedTask{… echoed fields …}, nil)` | Reset `backoff` to `BackoffMin`; increment `Stats.Locked`; log `task locked`; invoke `ProcessTask`; on success, call `MarkCompleted`. |
| Row was no longer claimable (status changed mid-flight; competing owner won) | `(nil, nil)` | Treat as no-op: do **not** invoke `ProcessTask`, do **not** increment `Stats.Locked`, do **not** record an error. Continue to next iteration. |
| Context cancelled / lease lost | `(nil, error)` (`context.Canceled`) | Exit the partition loop. |
| Transient backend error | `(nil, error)` (non-`context.Canceled`) | Increment `Stats.Errors`, log at `warn`, sleep `backoff`. |

### Conditional-claim semantics (binding)

The implementation MUST execute, inside a single serializable read-write transaction:

1. Point-select the row by full PK.
2. If the row does not exist → return `(nil, nil)`.
3. If `status = 'pending'`, **or** `status = 'locked'` AND `locked_until < CurrentUtcTimestamp()` → proceed to update; otherwise → return `(nil, nil)`.
4. UPDATE the row: `status = 'locked'`, `lock_value = $lockValue`, `locked_until = $lockedUntil`.

The transaction setting MUST be `query.WithSerializableReadWrite()` (preserved from current code). This is the only place in the worker package that performs a serializable read-write transaction for claiming.

### Implementation constraints

- MUST NOT generate the lock value or deadline — both are caller-supplied (FR-003).
- MUST NOT log or update metrics.
- The returned `ClaimedTask` MUST echo `c.ID`, `c.Payload`, `c.Priority`, `partitionID`, `lockValue`, `lockedUntil` verbatim. Implementations MUST NOT mutate these values.

---

## Method 3: `MarkCompleted`

```go
MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error
```

### Purpose

Transition a previously-claimed task to `status = 'completed'` and record the completion timestamp.

### Inputs

- `ctx`: cancellation signal — propagated as before.
- `task`: a `ClaimedTask` previously returned by a successful `ClaimTask`. Used for its (`PartitionID`, `Priority`, `ID`) PK; other fields are ignored by this method.
- `doneAt`: absolute UTC completion timestamp. Caller-supplied.

### Outcomes

| Outcome | Return | Worker reaction |
| ------- | ------ | --------------- |
| Update committed | `nil` | Increment `Stats.Processed`; log `task completed`. |
| Context cancelled | non-nil `error` (`context.Canceled`) | Drop the completion (the lock will expire and the row reverts to claimable). Do not record this as an error. |
| Transient backend error | non-nil `error` | Increment `Stats.Errors`; log at `warn`. The lock will expire and the row reverts to claimable. |

### Implementation constraints

- MUST update via point-write keyed on full PK (`partition_id`, `priority`, `id`).
- MUST run inside a serializable read-write transaction (`query.WithSerializableReadWrite()`), preserving the current behavior (FR-007).
- MUST NOT condition the update on the lock value (current behavior). A future spec amendment could add such a guard; the contract today is "key on PK only".
- MUST NOT log or update metrics.

---

## Cross-cutting invariants

- **No SQL outside the implementation**. The interface methods take and return only `string`, `uint8`, `uint16`, `time.Time`, and the two named structs — no `query.Session`, no `query.TxActor`, no `*ydb.Driver`. This is what enforces SC-001 (zero query strings or parameter-builder calls leak into `worker.go`).
- **No metrics, no logging in the repository**. FR-008 / FR-009. The Worker observes outcomes via the return values and decides what to log and what to count.
- **Errors carry context**. Transient errors MUST wrap the underlying YDB error so the worker's `slog.Warn(..., "err", err)` line is informative. Errors stemming from cancellation MUST satisfy `errors.Is(err, context.Canceled)` so the worker can distinguish them from transient backend errors (FR-011).
- **No retries inside the repository**. Each method runs one logical attempt. The Worker's outer backoff loop owns retry policy. This keeps the boundary observable: a test fake can return one outcome per call without simulating retry semantics.

## Test-time fake (informative)

The test fake (`fakeRepository` in `worker_test.go`) implements this interface against a scripted slice of outcomes. Indicative shape:

```go
type fakeOutcome struct {
    candidate *Candidate
    claimed   *ClaimedTask
    err       error
}

type fakeRepository struct {
    fetch    []fakeOutcome // one per FetchEligibleCandidate call
    claim    []fakeOutcome // one per ClaimTask call
    complete []error       // one per MarkCompleted call
    fetchIdx, claimIdx, completeIdx int
    completedTasks []ClaimedTask    // recorded for assertion
}
```

Tests assert on:

- The sequence of method invocations (e.g., "after a `nil, nil` from `FetchEligibleCandidate`, the worker MUST sleep and not call `ClaimTask`").
- The exact `ClaimedTask` passed to `MarkCompleted` (matching ID + lock value).
- That `ClaimTask` returning `nil, nil` skips `ProcessTask` and `MarkCompleted` entirely.
- That a processor error (`ProcessTask` returns non-nil) skips `MarkCompleted` (FR-007 / spec Edge Cases case 2).
- That `Stats.Errors` increments on transient errors but **not** on `nil, nil` outcomes.

The fake is private to `worker_test.go`; it is not part of the package's exported surface.
