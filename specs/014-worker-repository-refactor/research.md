# Phase 0 Research: Worker Repository Refactor

**Branch**: `014-worker-repository-refactor` | **Date**: 2026-04-25

The Technical Context in `plan.md` had no `NEEDS CLARIFICATION` items — the spec already pinned every functional requirement. The open questions are design choices about *how* to express the boundary, not unknown technologies. This document records the four design decisions that shape the interface, with rationale and rejected alternatives.

---

## Decision 1: Place the repository in the existing `taskworker` package, not a sibling package

**Decision**: New files (`repository.go`, `repository_ydb.go`, `worker_test.go`) live in `04_coordinated_table/pkg/taskworker/`, alongside the refactored `worker.go`.

**Rationale**:

- The current refactor scope is the worker only (spec User Story 3 / Assumptions). The producer's `UPSERT` against the same table is explicitly out of scope. A sibling package whose only consumer is one file would be premature.
- Same-package layout keeps the diff localised: `cmd/worker/main.go` does not gain a new import path, and the `Worker` struct does not gain a cross-package interface dependency before there is more than one consumer.
- The interface signatures in `repository.go` use only domain types (`uint16`, `string`, `time.Time`, `Candidate`, `ClaimedTask`) — no `taskworker.*` worker-internal types appear in them. If User Story 3 is later promoted, moving the two files to a sibling package is a mechanical rename plus a one-line import change in `worker.go` and `cmd/worker/main.go`.

**Alternatives considered**:

- **Sibling package `pkg/taskrepo/`**: Cleaner long-term boundary; rejected for now because it expands diff size, introduces a new public package surface for one current caller, and does work that User Story 3 explicitly defers.
- **Sub-package `pkg/taskworker/repository/`**: Violates the existing flat package convention (Constitution §I — even relaxed for this example, the existing structure has no nested packages under `pkg/`). Adds an import path with no visibility benefit.

---

## Decision 2: Three-outcome methods return `(*T, error)` with `nil, nil` as the no-op signal

**Decision**: Both `FetchEligibleCandidate` and `ClaimTask` return `(*Candidate, error)` and `(*ClaimedTask, error)` respectively, where `nil, nil` distinguishes "no eligible task / lost race" from "transient backend error". `MarkCompleted` returns `error` only.

**Rationale**:

- The current worker code already expresses this distinction implicitly via `if task == nil { backoff }; if err != nil { backoff && record error metric }` (`worker.go:144–149`). Preserving the pattern at the interface layer means orchestration logic in the refactored worker reads the same way it did before, satisfying FR-007 (no behavior change).
- A pointer-return contract is unambiguous in Go: callers cannot accidentally treat a zero `Candidate{}` as "found". `nil` is the only "not found" representation.
- Keeps error-typing simple: callers never need `errors.Is(err, ErrNotFound)` for the no-op case — and FR-011 explicitly says "no eligible task / lost race — which is not an error at all".

**Alternatives considered**:

- **`(Candidate, bool, error)` triple**: Idiomatic for map lookups in Go, but unconventional for repository methods and forces callers to discard the zero `Candidate{}` when `bool` is false — a common source of bugs.
- **Sentinel errors (`ErrNoEligibleTask`, `ErrLostRace`)**: Conflates "expected absence" with "error". Forces every caller to `errors.Is`-check, and FR-011 categorises the no-op outcome as *not an error*. Rejected.
- **Separate `TryClaim` returning `(bool, error)` + worker assembles `ClaimedTask`**: Pushes assembly logic back into the worker, partially defeating the abstraction (the worker would still need to know how `ClaimedTask` is constructed). The repository owns this assembly because it is the boundary that knows the claim succeeded.

---

## Decision 3: Caller supplies `lockValue` and `lockedUntil`; repository does not generate them

**Decision**: `ClaimTask(ctx, partitionID, candidate, lockValue, lockedUntil)` — the worker computes the UUID via `uid.GenerateUUID()` and the deadline via `time.Now().UTC().Add(w.LockDuration)` before calling, exactly as today.

**Rationale**:

- FR-003 explicitly says "caller-supplied lock value and lock-until deadline".
- `LockDuration` is a worker configuration concern (`*lockDurationFlag` in `cmd/worker/main.go`), not a storage concern. The repository should not import the worker's flags or duplicate the `time.Now()` clock.
- Tests can supply deterministic lock values (e.g., `"lock-1"`) and deadlines, making the fake repository's assertions exact rather than time-dependent.

**Alternatives considered**:

- **Repository takes only `lockDuration` and computes the deadline internally**: Hides clock state in the repository, complicates testing (would need a clock injection), and contradicts FR-003.
- **Repository generates the UUID**: Couples the storage layer to `github.com/google/uuid`, which currently lives in `pkg/uid` and is also used by the producer. Worker-layer concern.

---

## Decision 4: Test stand-in is an interface implementation, not a function-field hook

**Decision**: `Worker.Repo` is typed as `TaskRepository` (an interface). Tests construct a `fakeRepository` struct (in `worker_test.go`) that implements all three methods, with a scripted call sequence (slice of pre-canned outcomes) to exercise specific orchestration paths.

**Rationale**:

- FR-006 requires substitutability *as part of the boundary's shape*. An interface is the standard Go expression of substitutability and is what reviewers will expect.
- Three methods with three outcomes each (success / no-op / error) is small enough that a scripted slice-of-outcomes fake is straightforward (~50 LOC) — no need for `gomock` or `testify/mock`, which would add dependencies.
- The Worker already has one substitutable behavior (`ProcessTask func(ctx, taskID, payload) error`). The repository is the second; making it an interface keeps the substitution mechanism uniform per concept (one func field for the user-supplied processor, one interface for the storage boundary). Mixing function fields for storage would force the worker struct to grow three new fields.

**Alternatives considered**:

- **Three function fields on `Worker` (`FetchFn`, `ClaimFn`, `CompleteFn`)**: Avoids defining an interface, but bloats the `Worker` struct, makes the boundary harder to discover (a future reader sees three unrelated function fields), and obscures that they form a coherent contract. Rejected.
- **`gomock`-generated mock**: Adds tooling and a test dependency for three methods. Rejected as overkill.

---

## Method shape: final summary

```go
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

Three-outcome semantics per method are documented in `contracts/task_repository.md`.

---

## Open items resolved

| # | Question                                                                       | Resolution                                                              |
| - | ------------------------------------------------------------------------------ | ----------------------------------------------------------------------- |
| 1 | Where does the repository live?                                                | Same package (`pkg/taskworker`) — Decision 1                            |
| 2 | How are "no eligible task" / "lost race" / "transient error" distinguished?    | `nil, nil` for the first two; non-nil `error` for the third — Decision 2 |
| 3 | Who generates `lockValue` and `lockedUntil`?                                   | Worker — Decision 3                                                     |
| 4 | How do tests substitute an alternative implementation?                         | `TaskRepository` interface + hand-written fake — Decision 4             |
| 5 | Does the repository generate metrics or log lines?                             | No (FR-008, FR-009) — preserved from spec without further research      |
| 6 | Does the repository keep the existing two-phase locking?                       | Yes (FR-007) — preserved from spec; no research needed                  |

No `NEEDS CLARIFICATION` items remain. Ready for Phase 1.
