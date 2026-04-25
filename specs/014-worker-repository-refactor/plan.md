# Implementation Plan: Worker Repository Refactor

**Branch**: `014-worker-repository-refactor` | **Date**: 2026-04-25 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/014-worker-repository-refactor/spec.md`

## Summary

Extract every `coordinated_tasks` interaction currently embedded in `04_coordinated_table/pkg/taskworker/worker.go` (snapshot select, conditional claim transaction, completion transaction) into a single named abstraction — `TaskRepository` — that exposes three domain-level methods (`FetchEligibleCandidate`, `ClaimTask`, `MarkCompleted`). The Worker becomes a pure orchestrator: it reads partition events, drives backoff, invokes the user-supplied processor, and updates metrics/logs — but contains no SQL, no `query.ParamsBuilder`, no `query.WithTxControl`, and no result-set scanning. The interface is substitutable so orchestration logic (backoff escalation, lost-race no-op, lease cancellation, processor error path) becomes unit-testable without a live YDB instance. The two-phase locking strategy (snapshot RO select → serializable conditional update), the schema, the status vocabulary (`pending`/`locked`/`completed`), the structured-log fields, and the metrics counters are preserved verbatim — this is a code-only refactor.

## Technical Context

**Language/Version**: Go 1.26 (as declared in `go.mod`)
**Primary Dependencies**: `github.com/ydb-platform/ydb-go-sdk/v3 v3.127.0` (`query`, `query.Session`, `query.TxActor`, `ParamsBuilder`, `TxSettings`); stdlib `context`, `time`, `log/slog`. **No new direct `go.mod` dependencies.**
**Storage**: Existing `coordinated_tasks` table in YDB Serverless — schema, status vocabulary, and partition-key shape unchanged (FR-012).
**Testing**: Go stdlib `testing` package. New `worker_test.go` exercises orchestration via a hand-written fake `TaskRepository` (scripted call sequence) — no testcontainers, no live YDB, no goroutine leaks via `context.Cancel`. Integration validation remains manual (`go run ./04_coordinated_table/cmd/worker/`) per Constitution §IV.
**Target Platform**: Linux container (existing producer/worker VM image); developer machine for unit tests.
**Project Type**: In-place refactor of one Go package (`04_coordinated_table/pkg/taskworker`); no new top-level example directory.
**Performance Goals**: Throughput, lock-claim correctness, expired-lock reclaim, and lease-handover safety must remain within ±5% of pre-refactor values over a 10-minute steady-state run (SC-003).
**Constraints**: No new query patterns, no new transaction boundaries, no new statuses or columns (FR-007, FR-012). Logging stays at the worker layer (FR-008). Metrics stay at the worker layer (FR-009). Repository must NOT swallow `context.Canceled` (FR-011). Worker file must contain zero SQL strings, zero `ParamsBuilder` references, zero transaction-control settings (SC-001).
**Scale/Scope**: One package, four files after refactor: `worker.go` (orchestrator, ≤ 250 LOC; ≥ 30% shorter than current `lockNextTask`+`completeTask` per SC-002), `repository.go` (interface + types), `repository_ydb.go` (YDB implementation, owns all SQL), `worker_test.go` (orchestration tests). 256 logical partitions; existing producer rate (~hundreds–thousands of tasks/sec).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Verify compliance with each principle in `.specify/memory/constitution.md v1.0.0`:

| Principle | Check | Note |
| --------- | ----- | ---- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ⚠️ pre-existing | The `04_coordinated_table/` example was already split into `cmd/{producer,worker}` and `pkg/{taskproducer,taskworker,rebalancer,metrics,uid,ydbconn}` before this feature (see git log for 004-coordinated-table-workers). The refactor adds files inside the existing `pkg/taskworker/` package; it does not introduce new sub-packages and does not change the top-level layout. See Complexity Tracking. |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ | `cmd/worker/main.go` already wires `signal.NotifyContext`; this refactor does not touch `main.go`'s lifecycle. The Worker's per-partition cancellation paths are preserved verbatim. |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ | No schema changes (FR-012). Existing `coordinated_tasks` migration is untouched. |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ | No new env vars or flags. `cmd/worker/main.go` configuration is unchanged. |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ | All `slog.Info`/`slog.Warn` calls remain in `worker.go` with identical fields (`worker_id`, `partition_id`, `task_id`, `priority`, `err`). Repository emits no log lines (FR-008). |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ | Go 1.26, ydb-go-sdk/v3 v3.127.0 — already in `go.mod`. No new direct dependencies. murmur3 not used by worker (producer-only). |

## Project Structure

### Documentation (this feature)

```text
specs/014-worker-repository-refactor/
├── plan.md              # This file
├── research.md          # Phase 0 — design questions resolved
├── data-model.md        # Phase 1 — Candidate, ClaimedTask, TaskRepository surface
├── contracts/
│   └── task_repository.md   # Phase 1 — method-by-method contract for the interface
├── quickstart.md        # Phase 1 — how a reviewer/tester verifies the refactor
└── tasks.md             # Phase 2 — produced by /speckit.tasks (NOT this command)
```

### Source Code (repository root)

```text
04_coordinated_table/
├── cmd/
│   └── worker/
│       └── main.go              # MODIFIED: constructs ydbTaskRepository, passes via Worker.Repo
├── pkg/
│   └── taskworker/
│       ├── worker.go            # REFACTORED: orchestrator only — no SQL, no ParamsBuilder, no Tx settings
│       ├── repository.go        # NEW: TaskRepository interface + Candidate, ClaimedTask types
│       ├── repository_ydb.go    # NEW: ydbTaskRepository — owns all SQL, ParamsBuilder, TxSettings
│       └── worker_test.go       # NEW: orchestration tests using fakeRepository (scripted)
└── (cmd/producer, pkg/{taskproducer,rebalancer,metrics,uid,ydbconn} — UNCHANGED)
```

**Structure Decision**: All new files live inside the existing `04_coordinated_table/pkg/taskworker` package. We do **not** create a sibling package (`taskrepo/`, `coordinatedtasks/`) for this refactor:

- The current scope is the worker only; the producer's `UPSERT` is explicitly out of scope (spec User Story 3 / Assumptions). Promoting the repository to a sibling package now would be premature for a producer that we are not refactoring in this branch.
- Same-package layout keeps the diff small, the import graph unchanged, and identifier visibility (`Candidate`, `ClaimedTask`, `TaskRepository`, `NewYDBRepository`) discoverable to the existing `cmd/worker/main.go` without a new import path.
- If User Story 3 is later promoted to scope, moving `repository.go` + `repository_ydb.go` out to a sibling package is a mechanical rename — the interface shape we land here is designed to support that move (no `taskworker.*` types appear in the interface signatures).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
| --------- | ---------- | ------------------------------------ |
| Principle I (Self-Contained Examples) — `04_coordinated_table` uses `cmd/`+`pkg/` rather than a single `main.go` | Pre-existing structure ratified by feature 004-coordinated-table-workers; this refactor preserves it. The example combines a producer, a worker, a coordination-node rebalancer, and a metrics server — a single `main.go` would exceed ~1500 LOC and conflate four separate runnable binaries. | Collapsing into a single `main.go` was rejected by 004-coordinated-table-workers because the producer and worker are independent processes (separate VMs in the deployed system). This refactor cannot revert that decision and does not seek to. |
| Adding `worker_test.go` (the constitution describes validation as manual end-to-end) | The whole point of the refactor (User Story 2, FR-006, SC-004) is to make orchestration testable without a live database. Unit tests are the verification mechanism for SC-004 and the only way to exercise the lost-race / lease-cancelled / processor-error paths deterministically. | Constitution §"Development Workflow" describes manual validation as the baseline; it does not forbid additional unit tests, and the spec explicitly requires them. We treat the new test file as additive, not substitutive — manual `go run ./04_coordinated_table/cmd/worker/` validation against a live YDB still gates merge per SC-003. |
