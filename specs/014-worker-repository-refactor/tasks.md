# Tasks: Worker Repository Refactor

**Input**: Design documents from `/specs/014-worker-repository-refactor/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/task_repository.md ✅, quickstart.md ✅

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)

---

## Phase 1: Setup (Pre-Refactor Baseline)

**Purpose**: Establish LOC baseline needed for SC-002 verification before any refactoring begins.

- [ ] T001 Count non-blank non-comment lines in `lockNextTask` and `completeTask` functions in `04_coordinated_table/pkg/taskworker/worker.go` using `awk '/^func.*lockNextTask/,/^func/' worker.go | grep -cvE '^\s*$|^\s*//'` and record the total (expected ~209 LOC across both functions) — this baseline is required to verify SC-002 (≥30% reduction) after the refactor

---

## Phase 2: Foundational (Repository Contract)

**Purpose**: Define the `TaskRepository` interface and domain types that ALL subsequent phases depend on. No user story work can begin until T002 is complete.

**⚠️ CRITICAL**: T002 must be complete before any Phase 3 or Phase 4 work begins.

- [ ] T002 Create `04_coordinated_table/pkg/taskworker/repository.go` with package declaration `package taskworker`, import block (`context`, `time`), `Candidate` struct (`ID string`, `Priority uint8`, `Payload string`), `ClaimedTask` struct (`ID string`, `PartitionID uint16`, `Priority uint8`, `Payload string`, `LockValue string`, `LockedUntil time.Time`), and `TaskRepository` interface with exactly three methods: `FetchEligibleCandidate(ctx context.Context, partitionID uint16) (*Candidate, error)`, `ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error)`, `MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error` — matches contracts/task_repository.md exactly
- [ ] T003 Add `Repo TaskRepository` field to the `Worker` struct in `04_coordinated_table/pkg/taskworker/worker.go` (single field addition, no other changes) so US1 implementation and US2 test authoring can proceed concurrently after this point

**Checkpoint**: `repository.go` compiles and `Worker` struct has `Repo` field — unblocks Phase 3 and Phase 4.

---

## Phase 3: User Story 1 — Isolate Data Access Behind Repository (Priority: P1) 🎯 MVP

**Goal**: Every `coordinated_tasks` interaction moves out of `worker.go` into `repository_ydb.go`; the worker file is left with zero SQL strings, zero `ParamsBuilder` references, and zero `TxControl`/`TxSettings` calls.

**Independent Test**: Run the SC-001 grep from `quickstart.md` section 1 against `worker.go` — expect zero matches. Run the SC-005 grep — expect all column-name matches confined to `repository_ydb.go`. Run `go vet ./04_coordinated_table/...`. Then (with YDB access) run `go run ./04_coordinated_table/cmd/worker/` and observe tasks being locked and completed at the same rate as the pre-refactor baseline.

- [ ] T004 [P] [US1] Create `04_coordinated_table/pkg/taskworker/repository_ydb.go` with package `taskworker`, a `ydbTaskRepository` struct holding `db *ydb.Driver` (import `github.com/ydb-platform/ydb-go-sdk/v3`), and a `NewYDBRepository(db *ydb.Driver) TaskRepository` constructor that returns `&ydbTaskRepository{db: db}` — no method bodies yet, just the struct and constructor compiling against the interface stub (add empty method stubs to satisfy the interface if needed)
- [ ] T005 [US1] Implement `FetchEligibleCandidate` in `04_coordinated_table/pkg/taskworker/repository_ydb.go`: open a `query.Do` session with `query.SnapshotReadOnlyTxControl()`, execute a `DECLARE`+`SELECT` query on `coordinated_tasks` filtering `partition_id = $partitionID` AND (`status = 'pending'` OR (`status = 'locked'` AND `locked_until < CurrentUtcTimestamp()`)) AND (`scheduled_at IS NULL` OR `scheduled_at <= CurrentUtcTimestamp()`), `ORDER BY priority DESC LIMIT 1`; scan result into `*Candidate{ID, Priority, Payload}`; return `(nil, nil)` when result set is empty; propagate `ctx.Err()` on cancellation; wrap other errors — preserves exact query shape from pre-refactor `lockNextTask`
- [ ] T006 [US1] Implement `ClaimTask` in `04_coordinated_table/pkg/taskworker/repository_ydb.go`: open a `query.Do` session with `query.WithSerializableReadWrite()`, point-select row by full PK (`partition_id=$partitionID`, `priority=$priority`, `id=$id`); if no row or row is not claimable (status not in `{pending, locked-and-expired}`) return `(nil, nil)`; otherwise execute UPDATE setting `status='locked'`, `lock_value=$lockValue`, `locked_until=$lockedUntil`; on success return `&ClaimedTask{ID: c.ID, PartitionID: partitionID, Priority: c.Priority, Payload: c.Payload, LockValue: lockValue, LockedUntil: lockedUntil}` — preserves two-phase locking strategy from pre-refactor (FR-007)
- [ ] T007 [US1] Implement `MarkCompleted` in `04_coordinated_table/pkg/taskworker/repository_ydb.go`: open a `query.Do` session with `query.WithSerializableReadWrite()`, execute UPDATE on `coordinated_tasks` setting `status='completed'`, `done_at=$doneAt` WHERE `partition_id=$partitionID AND priority=$priority AND id=$id` (full PK, no lock-value condition per contract); return nil on success, wrapped error on failure — preserves exact completion transaction from pre-refactor `completeTask`
- [ ] T008 [US1] Refactor `04_coordinated_table/pkg/taskworker/worker.go`: replace the body of `lockNextTask` with a call sequence `w.Repo.FetchEligibleCandidate` → `w.Repo.ClaimTask` (with caller-supplied `uid.GenerateUUID()` lockValue and `time.Now().UTC().Add(w.LockDuration)` lockedUntil) → invoke `w.ProcessTask` → `w.Repo.MarkCompleted`; move all `slog.Info`/`slog.Warn` lines and all `w.Stats.*` increments to the orchestration layer in `worker.go`; delete `lockNextTask` and `completeTask` function bodies and their embedded SQL strings, `ParamsBuilder` calls, and `query.WithTxControl`/`query.WithSerializableReadWrite`/`query.SnapshotReadOnlyTxControl` calls; the resulting function MUST be named `processPartition` (or the existing name, preserving the existing caller) and MUST contain zero SQL strings
- [ ] T009 [US1] Update `04_coordinated_table/cmd/worker/main.go`: after constructing the `*ydb.Driver` (`db`), call `repo := taskworker.NewYDBRepository(db)` and assign `worker.Repo = repo` (or pass via the `Worker` constructor if one exists) — this is the sole composition-root change required; all other `main.go` wiring is unchanged

**Checkpoint**: `go vet ./04_coordinated_table/...` exits 0. SC-001 grep on `worker.go` returns zero matches. SC-005 grep returns zero matches outside `repository_ydb.go`. Worker binary compiles and (with YDB) processes tasks correctly.

---

## Phase 4: User Story 2 — Testable Worker Without a Live Database (Priority: P2)

**Goal**: A `fakeRepository` implementing `TaskRepository` enables all six orchestration paths to be exercised in `go test` with no database connection.

**Independent Test**: `unset YDB_ENDPOINT YDB_DATABASE && go test ./04_coordinated_table/pkg/taskworker/...` must pass and output `ok` with no skips. All six test cases from `quickstart.md` section 2 must be present and green.

- [ ] T010 [US2] Create `04_coordinated_table/pkg/taskworker/worker_test.go` with package `taskworker`, define `fakeOutcome` struct (`candidate *Candidate`, `claimed *ClaimedTask`, `err error`), define `fakeRepository` struct with fields `fetchOutcomes []fakeOutcome`, `claimOutcomes []fakeOutcome`, `completeErrors []error`, `fetchIdx int`, `claimIdx int`, `completeIdx int`, `completedTasks []ClaimedTask`; implement all three `TaskRepository` methods: each advances its index and returns the scripted outcome; `MarkCompleted` also appends `task` to `completedTasks` for assertion — this struct is the shared harness for all subsequent test cases
- [ ] T011 [US2] Add `TestBackoffEscalatesOnEmptyPolls` to `04_coordinated_table/pkg/taskworker/worker_test.go`: construct a `Worker` with a `fakeRepository` whose `fetchOutcomes` are all `{nil,nil,nil}` for N iterations; run the worker partition loop (cancel context after N iterations); assert that the sleep durations recorded by the worker grow from `BackoffMin` toward `BackoffMax` (each iteration doubles until capped); assert `ProcessTask` was never called (use a `processFn` that records calls)
- [ ] T012 [US2] Add `TestLostRaceNoOp` to `04_coordinated_table/pkg/taskworker/worker_test.go`: `fakeRepository` returns a valid `*Candidate` from fetch and `(nil,nil)` from claim; run one iteration; assert `ProcessTask` never called; assert `fakeRepository.completedTasks` is empty; assert `Stats.Errors` is 0; assert `Stats.Locked` is 0
- [ ] T013 [US2] Add `TestSuccessfulClaimProcessComplete` to `04_coordinated_table/pkg/taskworker/worker_test.go`: `fakeRepository` returns a valid `*Candidate` from fetch, a valid `*ClaimedTask` from claim, and `nil` from complete; `ProcessTask` returns nil; run one iteration; assert `fakeRepository.completedTasks` has exactly one entry with `ID` and `LockValue` matching the `ClaimedTask`; assert `Stats.Processed` is 1; assert `Stats.Locked` is 1
- [ ] T014 [US2] Add `TestProcessorErrorSkipsCompletion` to `04_coordinated_table/pkg/taskworker/worker_test.go`: `fakeRepository` returns candidate and claimed task; `ProcessTask` returns `errors.New("processing failed")`; run one iteration; assert `fakeRepository.completedTasks` is empty (MarkCompleted never called); assert `Stats.Errors` is 1; assert backoff was NOT reset (sleep duration after the iteration equals or exceeds `BackoffMin`, not reset to zero)
- [ ] T015 [US2] Add `TestTransientRepositoryError` to `04_coordinated_table/pkg/taskworker/worker_test.go`: `fakeRepository` fetch returns `(nil, errors.New("ydb: transient error"))`; run one iteration; assert `Stats.Errors` is 1; assert backoff increased (next sleep > `BackoffMin`); assert iteration continues (no panic, context not cancelled)
- [ ] T016 [US2] Add `TestLeaseCancellationExitsCleanly` to `04_coordinated_table/pkg/taskworker/worker_test.go`: construct a worker with a lease `context.Context` that is already cancelled (or cancel it immediately); run the partition loop; assert the function returns without error; assert `Stats.Errors` is 0 (cancellation is not an error metric)

**Checkpoint**: `go test ./04_coordinated_table/pkg/taskworker/...` with no `YDB_ENDPOINT` set passes all six test cases.

---

## Phase 5: User Story 3 — Centralized Table Contract (Priority: P3)

**Goal**: Verify (not implement new code) that all `coordinated_tasks` column names, status literals, and the eligibility predicate are confined to `repository_ydb.go` — the architecture invariant SC-005.

**Independent Test**: The SC-005 grep from `quickstart.md` returns zero matches outside `repository_ydb.go`.

- [ ] T017 [US3] Run the SC-005 verification grep from `quickstart.md` section 1: `grep -nE 'coordinated_tasks|locked_until|lock_value|partition_id|status\s*=\s*.pending|scheduled_at' 04_coordinated_table/pkg/taskworker/*.go | grep -v 'repository_ydb.go'`; if any match appears in `worker.go` or `repository.go`, move the reference into `repository_ydb.go` (this is a fix task if US1 left any stray string literals)

**Checkpoint**: SC-005 grep produces zero output. US3 acceptance scenarios are satisfied by inspection.

---

## Final Phase: Polish & Validation

**Purpose**: Build gate, static checks, and unit test run — verify all success criteria pass before merge.

- [ ] T018 [P] Run build gate: `go build -o /dev/null ./04_coordinated_table/cmd/worker/ && go build -o /dev/null ./04_coordinated_table/cmd/producer/` — both must exit 0; fix any compilation errors before proceeding
- [ ] T019 [P] Run `go vet ./04_coordinated_table/...` — must exit 0; fix any vet warnings
- [ ] T020 [P] Run SC-001 static check: `grep -nE 'SELECT|UPDATE|UPSERT|DECLARE|ParamsBuilder|WithTxControl|WithTxSettings|SerializableReadWrite|SnapshotReadOnly' 04_coordinated_table/pkg/taskworker/worker.go` — expect zero matches; if any match found, the refactor in T008 is incomplete
- [ ] T021 Run SC-002 line count: `awk '/^func \(w \*Worker\) processPartition/,/^}/' 04_coordinated_table/pkg/taskworker/worker.go | grep -cvE '^\s*$|^\s*//'` — result must be ≤ 145 (≥30% reduction from the ~209 LOC baseline recorded in T001); if over target, simplify the orchestration logic
- [ ] T022 Run unit tests without database: `unset YDB_ENDPOINT YDB_DATABASE && go test ./04_coordinated_table/pkg/taskworker/...` — must print `ok` with all 6 test cases passing; if any test fails, fix the corresponding orchestration logic in `worker.go`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — run immediately (T001)
- **Foundational (Phase 2)**: Depends on Phase 1 only — T002 must complete before Phase 3 or 4 can start; T003 can follow T002 immediately
- **US1 (Phase 3)**: Depends on T002 and T003 — T004 and T005–T007 can proceed in order; T008 requires T005–T007 complete; T009 requires T008
- **US2 (Phase 4)**: Depends on T002 and T003 — T010 must precede T011–T016; T011–T016 can be written sequentially in `worker_test.go` (same file)
- **US3 (Phase 5)**: Depends on Phase 3 complete — T017 verifies the outcome of T008
- **Polish (Final Phase)**: Depends on Phase 3, 4, and 5 complete — T018/T019/T020 can run in parallel; T021 and T022 follow

### User Story Dependencies

- **US1 (P1)**: Start after T002+T003 — no dependency on US2 or US3
- **US2 (P2)**: Start after T002+T003 — can be worked concurrently with US1 (different files: `worker_test.go` vs `repository_ydb.go`); depends on T008 to be complete before tests can be run (need the refactored worker to test against)
- **US3 (P3)**: Depends on US1 complete — no new code, only verification

### Parallel Opportunities

- T004 (`repository_ydb.go` skeleton) can be written in parallel with T008 prep (reading existing `worker.go`)
- T010 (`fakeRepository` struct) can be written in parallel with T004–T007 since it only depends on `repository.go` (T002)
- T018, T019, T020 (build, vet, SC-001) can all run in parallel in the final phase

---

## Parallel Example: US1 + US2 Concurrent Start

```
After T002 + T003 complete:

  Stream A (US1):
    T004 → T005 → T006 → T007 → T008 → T009 → T017

  Stream B (US2):
    T010 → [T011–T016 sequentially in worker_test.go]
    (run go test only after T008 is complete)
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Baseline (T001)
2. Complete Phase 2: Foundational (T002, T003) — **BLOCKS everything**
3. Complete Phase 3: US1 (T004–T009)
4. **STOP and VALIDATE**: SC-001 grep, SC-005 grep, `go vet`, `go build`
5. Merge or demo if static checks pass

### Incremental Delivery

1. T001 → T002 → T003: Foundation ready
2. T004–T009: Repository boundary + worker refactored → static checks pass → MVP
3. T010–T016: Orchestration tests → `go test` passes without YDB → SC-004 met
4. T017: SC-005 verified → US3 complete
5. T018–T022: All success criteria confirmed → ready for merge

---

## Notes

- Tests are **required** for this feature: `worker_test.go` is a named deliverable in `plan.md` and `quickstart.md` specifies the exact 6 test cases
- `[P]` marks tasks that touch **different files** with no incomplete dependencies — safe to execute concurrently
- `[US*]` label maps each task to its user story for traceability
- After T008 the Worker struct MUST contain zero SQL strings — verify with SC-001 grep before proceeding to Phase 4
- Never use `go build ./04_coordinated_table/...` from inside the package directory — always use `-o /dev/null` or `go vet` (project convention)
- The repository must NOT swallow `context.Canceled` — FR-011; verify in T015 and T016 test cases
