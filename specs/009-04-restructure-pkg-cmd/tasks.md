# Tasks: Restructure 04 Example into pkg/cmd Layout

**Input**: Design documents from `/specs/009-04-restructure-pkg-cmd/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅, quickstart.md ✅

**Organization**: Tasks are grouped by phase. Phase 2 (Foundational) extracts all `pkg/` packages
— this blocks both user story phases. US1 and US2 can then proceed in parallel.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)

---

## Phase 1: Setup (Directory Structure)

**Purpose**: Create the `cmd/` and `pkg/` directory skeleton before any code is moved.

- [X] T001 Create directory structure: `04_coordinated_table/cmd/producer/`, `04_coordinated_table/cmd/worker/`, `04_coordinated_table/pkg/uid/`, `04_coordinated_table/pkg/metrics/`, `04_coordinated_table/pkg/rebalancer/`, `04_coordinated_table/pkg/taskworker/`, `04_coordinated_table/pkg/taskproducer/`, `04_coordinated_table/pkg/ydbconn/` (use `mkdir -p`; no Go files yet)

**Checkpoint**: Directory skeleton exists; no build changes yet.

---

## Phase 2: Foundational (Extract Shared Packages)

**Purpose**: Extract all reusable logic from the flat `package main` into six `pkg/` packages. Both binary entry points (US1, US2) import from here — this phase MUST complete first.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T002 [P] Create `04_coordinated_table/pkg/uid/uid.go` — package `uid`; move `generateUUID() (string, error)` verbatim from `04_coordinated_table/utils.go`; import path `async-tasks-ydb/04_coordinated_table/pkg/uid`

- [X] T003 [P] Create `04_coordinated_table/pkg/metrics/handler.go` — package `metrics`; move `metricsHandler(registry *prometheus.Registry) http.Handler` verbatim from `04_coordinated_table/metrics.go`; imports `net/http` and `prometheus/client_golang`

- [X] T004 [P] Create `04_coordinated_table/pkg/metrics/worker_stats.go` — package `metrics`; move `Stats` struct and its methods (`newStats`, `readCounter`, `readGauge`, `display`) from `04_coordinated_table/display.go`; export all identifiers that are referenced outside the package (`Stats`, `NewStats`, `Display`)

- [X] T005 [P] Create `04_coordinated_table/pkg/metrics/producer_stats.go` — package `metrics`; move `ProducerStats` struct and `newProducerStats` constructor verbatim from `04_coordinated_table/producer_stats.go`; export both identifiers (`ProducerStats`, `NewProducerStats`)

- [X] T006 [P] Create `04_coordinated_table/pkg/rebalancer/rebalancer.go` — package `rebalancer`; move `Rebalancer`, `PartitionEvent` (exported — consumed by `pkg/taskworker`), `newRebalancer` → `NewRebalancer`, `ceilDiv`, `start` → `Start`, `stop` → `Stop` from `04_coordinated_table/rebalancer.go`; fix import paths

- [X] T007 [P] Create `04_coordinated_table/pkg/taskworker/worker.go` — package `taskworker`; move `Worker`, `lockedTask` (unexported), `minDuration` (unexported), and `run` → `Run` method from `04_coordinated_table/worker.go`; import `async-tasks-ydb/04_coordinated_table/pkg/metrics` and `async-tasks-ydb/04_coordinated_table/pkg/rebalancer`; channel parameter type changes to `<-chan rebalancer.PartitionEvent`

- [X] T008 [P] Create `04_coordinated_table/pkg/taskproducer/producer.go` — package `taskproducer`; move `taskRow` (unexported), `buildBatch`, `upsertBatch`, `Produce` from `04_coordinated_table/producer.go`; import `async-tasks-ydb/04_coordinated_table/pkg/uid` and `async-tasks-ydb/04_coordinated_table/pkg/metrics`

- [X] T009 [P] Create `04_coordinated_table/pkg/ydbconn/conn.go` — package `ydbconn`; extract credential-resolution logic + YDB driver creation from `04_coordinated_table/main.go` into `Open(ctx context.Context, endpoint, database string) (*ydb.Driver, error)`; resolves: `YDB_SA_KEY_FILE` → `yc.WithServiceAccountKeyFileCredentials`, `YDB_ANONYMOUS_CREDENTIALS=1` → `ydb.WithAnonymousCredentials`, default → `yc.WithMetadataCredentials`

**Checkpoint**: Run `go vet ./04_coordinated_table/pkg/...` (expect success once all T002–T009 files exist). The old flat files are still present — do not delete them yet.

---

## Phase 3: User Story 1 — Run Producer as Standalone Binary (Priority: P1) 🎯 MVP

**Goal**: A developer can build and run the producer binary independently using `go run ./04_coordinated_table/cmd/producer/` with only producer-relevant flags.

**Independent Test**: `go build -o /dev/null ./04_coordinated_table/cmd/producer/` succeeds; running with `--endpoint` and `--database` starts the producer; passing `--lock-duration` exits with "flag provided but not defined".

### Implementation for User Story 1

- [X] T010 [US1] Create `04_coordinated_table/cmd/producer/main.go` — package `main`; define flags per `contracts/producer-cli.md`: `--endpoint` (default `$YDB_ENDPOINT`), `--database` (default `$YDB_DATABASE`), `--partitions` (256), `--coordination-path` (unused, kept for parity), `--rate` (100), `--batch-window` (100ms), `--report-interval` (5s), `--metrics-port` (9090); validate `--endpoint` and `--database` (exit 1 with `slog.Error` if absent); call `ydbconn.Open` → start Prometheus HTTP server → call `taskproducer.Produce`; handle `SIGTERM`/`SIGINT` via `signal.NotifyContext`; import all required `pkg/` packages; do NOT define `--lock-duration`, `--backoff-min`, `--backoff-max`, or `--mode`

**Checkpoint**: `go build -o /dev/null ./04_coordinated_table/cmd/producer/` passes. User Story 1 independently testable.

---

## Phase 4: User Story 2 — Run Worker as Standalone Binary (Priority: P1)

**Goal**: A developer can build and run the worker binary independently using `go run ./04_coordinated_table/cmd/worker/` with only worker-relevant flags.

**Independent Test**: `go build -o /dev/null ./04_coordinated_table/cmd/worker/` succeeds; running with `--endpoint` and `--database` starts the worker; passing `--rate` exits with "flag provided but not defined".

### Implementation for User Story 2

- [X] T011 [US2] Create `04_coordinated_table/cmd/worker/main.go` — package `main`; define flags per `contracts/worker-cli.md`: `--endpoint` (default `$YDB_ENDPOINT`), `--database` (default `$YDB_DATABASE`), `--partitions` (256), `--coordination-path` (default `<database>/04_coordinated_table`), `--lock-duration` (5s), `--backoff-min` (50ms), `--backoff-max` (5s), `--metrics-port` (9090); validate `--endpoint` and `--database` (exit 1 with `slog.Error` if absent); call `ydbconn.Open` → `db.Coordination().CreateNode(...)` → start Prometheus HTTP server → `rebalancer.NewRebalancer(...).Start(ctx)` → construct `taskworker.Worker` and call `Worker.Run`; define local `newUUID()` panic-wrapper (calls `uid.GenerateUUID`); handle `SIGTERM`/`SIGINT` via `signal.NotifyContext`; log `slog.Info("worker shutdown complete", "worker_id", ...)` on exit; do NOT define `--rate`, `--batch-window`, `--report-interval`, or `--mode`

**Checkpoint**: `go build -o /dev/null ./04_coordinated_table/cmd/worker/` passes. User Stories 1 AND 2 both independently testable.

---

## Phase 5: User Story 3 — Reuse Shared Logic via pkg (Priority: P2)

**Goal**: Verify zero code duplication between `cmd/` entry points and `pkg/`; confirm that changing a shared type recompiles both binaries. Delete the now-superseded flat-layout source files.

**Independent Test**: `go vet ./04_coordinated_table/...` succeeds; `grep -r "package main" 04_coordinated_table/pkg/` returns nothing; no logic that exists in `pkg/` is copy-pasted into `cmd/`.

### Implementation for User Story 3

- [X] T012 [US3] Delete superseded flat-layout files: `04_coordinated_table/main.go`, `04_coordinated_table/display.go`, `04_coordinated_table/metrics.go`, `04_coordinated_table/producer_stats.go`, `04_coordinated_table/producer.go`, `04_coordinated_table/rebalancer.go`, `04_coordinated_table/utils.go`, `04_coordinated_table/worker.go` — verify each file's logic has been fully migrated to `pkg/` or `cmd/` before deleting

- [X] T013 [US3] Run `go vet ./04_coordinated_table/...` from repo root and resolve any remaining import or type errors revealed by the deletion of old files

**Checkpoint**: All three user stories complete. Both binaries build clean; no source duplication.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation update and final end-to-end build validation.

- [X] T014 Update `04_coordinated_table/README.md` to reflect the new layout: replace `go run ./04_coordinated_table/` with `go run ./04_coordinated_table/cmd/producer/` and `go run ./04_coordinated_table/cmd/worker/`; document all flags from `contracts/producer-cli.md` and `contracts/worker-cli.md`; include the quickstart build commands from `quickstart.md`

- [X] T015 [P] Verify final build: run `go build -o /dev/null ./04_coordinated_table/cmd/producer/` and `go build -o /dev/null ./04_coordinated_table/cmd/worker/` from repo root; confirm both succeed with zero errors

- [X] T016 [P] Smoke-test flag rejection: run `go run ./04_coordinated_table/cmd/worker/ --rate 100` and confirm exit with "flag provided but not defined: -rate"; run `go run ./04_coordinated_table/cmd/producer/ --lock-duration 5s` and confirm exit with "flag provided but not defined: -lock-duration"

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — start immediately
- **Phase 2 (Foundational)**: Depends on Phase 1 — BLOCKS Phase 3, 4, 5
- **Phase 3 (US1)**: Depends on Phase 2 completion
- **Phase 4 (US2)**: Depends on Phase 2 completion — parallel with Phase 3
- **Phase 5 (US3)**: Depends on Phase 3 AND Phase 4 completion
- **Phase 6 (Polish)**: Depends on Phase 5 completion

### User Story Dependencies

- **User Story 1 (P1)**: Starts after Phase 2 — no dependency on US2 or US3
- **User Story 2 (P1)**: Starts after Phase 2 — no dependency on US1 or US3; parallel with US1
- **User Story 3 (P2)**: Starts after US1 AND US2 complete (validates the combined result)

### Within Each Phase

- All [P]-marked tasks in Phase 2 can run in parallel (each targets a distinct file)
- T010 (US1) and T011 (US2) can run in parallel once Phase 2 is complete
- T012 must precede T013 (delete then vet)
- T015 and T016 can run in parallel once T012–T013 complete

---

## Parallel Example: Phase 2 (Foundational)

```bash
# All six pkg files can be created simultaneously:
Task T002: 04_coordinated_table/pkg/uid/uid.go
Task T003: 04_coordinated_table/pkg/metrics/handler.go
Task T004: 04_coordinated_table/pkg/metrics/worker_stats.go
Task T005: 04_coordinated_table/pkg/metrics/producer_stats.go
Task T006: 04_coordinated_table/pkg/rebalancer/rebalancer.go
Task T007: 04_coordinated_table/pkg/taskworker/worker.go
Task T008: 04_coordinated_table/pkg/taskproducer/producer.go
Task T009: 04_coordinated_table/pkg/ydbconn/conn.go
```

## Parallel Example: User Stories 1 & 2

```bash
# Once Phase 2 is done, both entry points can be created simultaneously:
Task T010: 04_coordinated_table/cmd/producer/main.go  (US1)
Task T011: 04_coordinated_table/cmd/worker/main.go    (US2)
```

---

## Implementation Strategy

### MVP First (User Story 1 — Producer Binary)

1. Complete Phase 1: Setup (directories)
2. Complete Phase 2: Foundational (pkg packages — critical blocker)
3. Complete Phase 3: User Story 1 (producer entry point)
4. **STOP and VALIDATE**: `go build -o /dev/null ./04_coordinated_table/cmd/producer/`
5. Proceed to Phase 4 if validated

### Incremental Delivery

1. Phase 1 + Phase 2 → shared packages ready
2. Phase 3 → producer binary works independently (MVP)
3. Phase 4 → worker binary works independently
4. Phase 5 → old files deleted; zero duplication confirmed
5. Phase 6 → README updated; final smoke tests pass

### Parallel Developer Strategy

With two developers after Phase 2:
- Developer A: T010 (cmd/producer/main.go — US1)
- Developer B: T011 (cmd/worker/main.go — US2)

---

## Notes

- No automated test suite per constitution; validation is manual build + flag smoke tests
- The Go module path prefix for all pkg imports is `async-tasks-ydb/04_coordinated_table/pkg/<name>`
- `partitionEvent` must be exported as `PartitionEvent` in `pkg/rebalancer` because `pkg/taskworker` receives it over a channel
- `newUUID` panic-wrapper stays in `cmd/worker/main.go` only (Decision 4 in research.md)
- `db.Coordination().CreateNode(...)` moves to `cmd/worker/main.go` only (Decision 3 in research.md)
- Use `go build -o /dev/null` (not `go build ./...`) per project build conventions
