# Tasks: Worker Task Processor — API Gateway Call

**Input**: Design documents from `/specs/010-yc-apigw-dummy/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2)
- Paths are relative to repo root

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Verify existing project structure and confirm readiness for changes.

- [X] T001 Verify `04_coordinated_table/pkg/` layout matches plan.md (cmd/producer, cmd/worker, pkg/metrics, pkg/taskproducer, pkg/taskworker)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core data-carrying and injection-point changes that both user stories depend on. Must complete before US1 or US2 work begins.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T002 Add `payload string` field to `lockedTask` struct in `04_coordinated_table/pkg/taskworker/worker.go`
- [X] T003 Extend `lockNextTask` SELECT to fetch `payload` column and scan it into `lockedTask.payload` in `04_coordinated_table/pkg/taskworker/worker.go`
- [X] T004 Add `ProcessTask func(ctx context.Context, taskID string, payload string) error` field to `Worker` struct in `04_coordinated_table/pkg/taskworker/worker.go`
- [X] T005 Replace `time.Sleep(100ms)` in `completeTask` with conditional `w.ProcessTask` invocation (nil-safe; on error log warn, increment `w.Stats.Errors`, return without completing) in `04_coordinated_table/pkg/taskworker/worker.go`

**Checkpoint**: `taskworker` now supports payload-aware processors; producer changes can now proceed in parallel with metric changes.

---

## Phase 3: User Story 1 — Inject real work into task processing (Priority: P1) 🎯 MVP

**Goal**: Producer embeds API Gateway URL in task payload JSON; worker unmarshal it, POSTs to it, and only marks task `completed` on HTTP 200.

**Independent Test**: Run producer with `--apigw-url`, run worker; confirm tasks reach `status='completed'` only after a successful HTTP POST, and that non-200 / parse errors leave the task `locked`.

### Implementation for User Story 1

- [X] T006 [P] [US1] Update `Produce` / `buildBatch` in `04_coordinated_table/pkg/taskproducer/producer.go` to accept `apigwURL string` and serialize payload as `{"url":<apigwURL>}`
- [X] T007 [P] [US1] Add `--apigw-url` flag (defaulting to `$APIGW_URL`) to `04_coordinated_table/cmd/producer/main.go`; fail fast with `slog.Error` + `os.Exit(1)` if empty; pass value to `taskproducer.Produce`
- [X] T008 [US1] Add `APIGWCalls *prometheus.CounterVec` field to `Stats` struct, create it with labels `["http_status"]` and const label `worker_id` in `NewStats`, and register it on the existing registry in `04_coordinated_table/pkg/metrics/worker_stats.go` (depends on T004)
- [X] T009 [US1] Implement `newAPIGWProcessor` closure in `04_coordinated_table/cmd/worker/main.go`: unmarshal payload JSON, build `http.NewRequestWithContext` POST with body=payload, `Content-Type: application/json`, `X-Task-ID` header; increment `stats.APIGWCalls` by status string or `"error"` on transport failure; return non-nil error on non-200 or parse failure (depends on T008)
- [X] T010 [US1] Wire `newAPIGWProcessor` into `worker.ProcessTask` before calling `worker.Run` in `04_coordinated_table/cmd/worker/main.go` (depends on T004, T009)

**Checkpoint**: User Story 1 fully functional — producer injects URLs, worker POSTs to them, tasks only complete on HTTP 200.

---

## Phase 4: User Story 2 — Observe per-task HTTP call outcomes in logs (Priority: P2)

**Goal**: Every HTTP call attempt emits a structured `slog` line containing `task_id`, `url`, `http_status` (or error), and final task outcome, so operators can trace results without querying the database.

**Independent Test**: Run worker, capture stdout, grep for `"apigw call"` log lines; confirm each contains `task_id` and `http_status`; confirm failed calls include error context and `outcome=error`.

### Implementation for User Story 2

- [X] T011 [US2] Add `slog.Info("apigw call", "task_id", taskID, "url", p.URL, "http_status", resp.StatusCode)` on successful HTTP 200 in the `newAPIGWProcessor` closure in `04_coordinated_table/cmd/worker/main.go` (depends on T009)
- [X] T012 [US2] Add `slog.Info("apigw call", "task_id", taskID, "url", p.URL, "http_status", "error", "err", err, "outcome", "error")` on transport error and `slog.Info("apigw call", "task_id", taskID, "url", p.URL, "http_status", resp.StatusCode, "outcome", "error")` on non-200 in `newAPIGWProcessor` in `04_coordinated_table/cmd/worker/main.go` (depends on T009)
- [X] T013 [US2] Add `slog.Info("apigw call", "task_id", taskID, "outcome", "error", "err", "invalid payload")` for malformed / URL-less payload case in `newAPIGWProcessor` in `04_coordinated_table/cmd/worker/main.go` (depends on T009)

**Checkpoint**: User Story 2 complete — structured log lines for all HTTP outcomes, independently verifiable from stdout.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: Build validation and acceptance baseline verification.

- [X] T014 Run `go vet ./...` from `04_coordinated_table/` and fix any reported issues across all changed packages
- [X] T015 Verify acceptance baseline manually: confirm `coordinator_apigw_calls_total{http_status="200"}` increments on `/metrics`, `"apigw call"` log lines appear, and `coordinated_tasks` rows reach `status='completed'`

---

## Dependencies

```
T001
└─ T002 → T003 → T004 → T005
                         ├─ T006 [P, US1]
                         ├─ T007 [P, US1]
                         ├─ T008 [US1] → T009 → T010
                         │                  ├─ T011 [US2]
                         │                  ├─ T012 [US2]
                         │                  └─ T013 [US2]
                         └─ T014 → T015
```

US2 (T011–T013) depends on T009 being in place; it can be implemented immediately after T009 without waiting for T010.

## Parallel Execution Opportunities

**Within Phase 2**: T002–T005 must be sequential (each extends the prior change in the same file).

**After Phase 2 completes**:
- T006 (producer payload) and T007 (producer flag) can run in parallel — different files.
- T008 (metrics field) can start immediately; T009 and T010 follow sequentially.

**After T009 completes**:
- T011, T012, T013 (logging) are all in the same file/function and must be sequential.

## Implementation Strategy

**MVP scope**: Phase 1 + Phase 2 + Phase 3 (T001–T010).

Once T010 is done the feature meets all P1 acceptance criteria. Phase 4 (T011–T013) adds observability logging for P2 and can be delivered as a follow-up increment without blocking the MVP.
