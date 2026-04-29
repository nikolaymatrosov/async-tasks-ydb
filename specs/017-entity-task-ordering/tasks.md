---
description: "Task list for Per-Entity Ordered Task Delivery (017)"
---

# Tasks: Per-Entity Ordered Task Delivery

**Input**: Design documents from `/specs/017-entity-task-ordering/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Tests are NOT requested (constitution: manual end-to-end validation per `quickstart.md`). No automated test tasks are emitted.

**Organization**: Tasks are grouped by user story so each can be implemented and validated independently against the quickstart scenarios.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: User story label (US1, US2, US3, US4)
- Paths are absolute repo-relative paths

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the new top-level example directories and skeleton files. No business logic yet.

- [X] T001 [P] Create directory skeleton `05_ordered_tasks/cmd/producer/`, `05_ordered_tasks/cmd/worker/`, `05_ordered_tasks/pkg/taskproducer/`, `05_ordered_tasks/pkg/taskworker/`, `05_ordered_tasks/pkg/rebalancer/`, `05_ordered_tasks/pkg/ydbconn/`, `05_ordered_tasks/pkg/uid/`, `05_ordered_tasks/pkg/metrics/` with empty `.gitkeep` files where needed
- [X] T002 [P] Create directory skeleton `06_target_server/` (single-file example per constitution principle I)
- [X] T003 [P] Create `05_ordered_tasks/README.md` describing the example (forked from `04_coordinated_table/`, per-entity ordering demo, single-instance producer, no topic, no relay)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Schema migration and shared packages copied/forked from `04_coordinated_table` that every subsequent user story depends on.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T004 Create goose migration `migrations/20260429000007_create_ordered_tasks.sql` with the full `CREATE TABLE ordered_tasks` (PK `(partition_id, id)`, columns per data-model §1) and the `idx_partition_entity_seq GLOBAL` covering index plus a symmetric `+goose Down` `DROP TABLE ordered_tasks`
- [X] T005 [P] Copy `04_coordinated_table/pkg/ydbconn/conn.go` to `05_ordered_tasks/pkg/ydbconn/conn.go` unchanged (YDB driver factory, env-var config)
- [X] T006 [P] Copy `04_coordinated_table/pkg/uid/uid.go` to `05_ordered_tasks/pkg/uid/uid.go` unchanged (UUID helper)
- [X] T007 [P] Copy `04_coordinated_table/pkg/rebalancer/` recursively to `05_ordered_tasks/pkg/rebalancer/` unchanged (256 partition semaphore leases via YDB coordination node)
- [X] T008 [P] Copy `04_coordinated_table/pkg/metrics/` recursively to `05_ordered_tasks/pkg/metrics/`, then add `entity_id` and `entity_seq` fields to whatever per-task structured-log helpers exist; do NOT reference `priority` anywhere
- [X] T009 Apply the migration against a dev YDB instance via `go run ./cmd/migrate up` and confirm via `ydb scheme describe` that table `ordered_tasks` and index `idx_partition_entity_seq` exist (quickstart §1)

**Checkpoint**: Foundation ready — user story implementation can now begin.

---

## Phase 3: User Story 1 — Strict in-order delivery per entity (Priority: P1) 🎯 MVP

**Goal**: Producer writes ordered rows; worker dispatches the head of each entity per partition, leaves successors invisible until the head reaches terminal success.

**Independent Test**: Submit `A`, `B`, `C` for entity `E1` with idle consumers; verify `B` is not dispatched until `A` succeeds and `C` is not dispatched until `B` succeeds, while different entities continue dispatching in parallel (quickstart §5, SC-001).

### Implementation for User Story 1

- [X] T010 [P] [US1] Define `Candidate`, `ClaimedTask`, and `TaskRepository` interface in `05_ordered_tasks/pkg/taskworker/repository.go` per data-model §2 (no `priority` field anywhere; include `EntityID`, `EntitySeq`, `AttemptCount`, `ScheduledAt *time.Time`, `LockedUntil *time.Time`)
- [X] T011 [P] [US1] Implement the producer in `05_ordered_tasks/pkg/taskproducer/producer.go`: process-wide `nextSeq()` using `uint64(time.Now().UnixNano())*1024 + atomic.AddUint64(&seqCounter, 1)`; entity pool `entity-0000000`…`entity-N-1` from `--entities`; per-task `partition_id = uint16(uint64(murmur3.Sum32([]byte(entityID))) % uint64(partitions))`; batch UPSERT via `AS_TABLE($records)` writing exactly the new schema (no `priority`, no `hash`); `payload = {"url":"https://${apigwURL}/"}`
- [X] T012 [US1] Implement `repository_ydb.FetchEligibleHeads(ctx, partitionID, k)` in `05_ordered_tasks/pkg/taskworker/repository_ydb.go` — `SELECT ... FROM ordered_tasks VIEW idx_partition_entity_seq WHERE partition_id=$pid AND status IN ('pending','locked') ORDER BY entity_id, entity_seq LIMIT $k`, returning a slice of `Candidate` (research §3, §4) (depends on T010)
- [X] T013 [US1] Implement `repository_ydb.ClaimTask(ctx, partitionID, c, lockValue, lockedUntil)` in `05_ordered_tasks/pkg/taskworker/repository_ydb.go` as a serializable CAS update conditioned on the row's current `status` and (if locked) lease expiry; on success returns a populated `*ClaimedTask`; on lost race returns `(nil, nil)` (research §4, §8) (depends on T010)
- [X] T014 [US1] Implement `repository_ydb.MarkCompleted(ctx, task, doneAt)` in `05_ordered_tasks/pkg/taskworker/repository_ydb.go` as a serializable update conditioned on `status='locked' AND lock_value=$lv`, setting `status='completed', done_at=$now, lock_value=NULL, locked_until=NULL` (research §8) (depends on T010)
- [X] T015 [US1] Implement the per-partition worker loop in `05_ordered_tasks/pkg/taskworker/worker.go`: dedup-by-entity over `FetchEligibleHeads` results, drop heads with `scheduled_at > now` or `(status='locked' AND locked_until > now)`, round-robin pick, attempt `ClaimTask`, on success invoke `ProcessTask(ctx, ClaimedTask) error`, on success call `MarkCompleted`; widen `ProcessTask` signature to take a `ClaimedTask` (contracts/target-server-ingest.md) (depends on T012, T013, T014)
- [X] T016 [US1] Wire the worker entrypoint in `05_ordered_tasks/cmd/worker/main.go`: flags `--partitions`, `--lock-duration`, `--backoff-min`, `--backoff-max`, `--max-attempts`, `--metrics-port`; build YDB connection, rebalancer with 256 semaphores, repository, per-partition workers; `signal.NotifyContext(SIGTERM, SIGINT)`; on shutdown print plain stats block (depends on T015)
- [X] T017 [US1] Wire the producer entrypoint in `05_ordered_tasks/cmd/producer/main.go`: flags `--rate`, `--partitions`, `--batch-window`, `--apigw-url`, `--entities`; rate-shaped batched UPSERT loop using `taskproducer`; `signal.NotifyContext(SIGTERM, SIGINT)`; on shutdown print stats block (`total_inserted` etc.) (depends on T011)
- [X] T018 [US1] In `05_ordered_tasks/cmd/worker/main.go`, implement `newAPIGWProcessor` returning `func(ctx, ClaimedTask) error` that POSTs `task.Payload` to the `payload.url` and sets headers `X-Task-ID`, `X-Entity-ID = task.EntityID`, `X-Entity-Seq = strconv.FormatUint(task.EntitySeq, 10)`, `Content-Type: application/json` (contracts/target-server-ingest.md §Worker-side change) (depends on T016)
- [X] T019 [US1] Validate end-to-end against quickstart §5: run producer + worker + target-server (fault injection off) for ~60 s; confirm `violation_total == 0` and a sample entity's `last_accepted_seq` strictly increases (depends on T017, T018, T036)

**Checkpoint**: User Story 1 fully functional — strict per-entity FIFO under healthy conditions, with cross-entity parallelism.

---

## Phase 4: User Story 2 — Backoff of head task blocks the entity's queue (Priority: P1)

**Goal**: When a head task fails transiently, the entity's successors stay invisible for the full backoff window; other entities are unaffected.

**Independent Test**: With target-server fault rate at 30 %, run producer + worker for 60 s; confirm worker emits `MarkFailedWithBackoff` paths, target server reports `violation_total == 0`, and 429 rate is within ±2 pp of configured (quickstart §6, SC-002, SC-003, SC-009).

### Implementation for User Story 2

- [X] T020 [US2] Implement `repository_ydb.MarkFailedWithBackoff(ctx, task, retryAt, lastError)` in `05_ordered_tasks/pkg/taskworker/repository_ydb.go` — serializable update conditioned on `status='locked' AND lock_value=$lv`, setting `status='pending', lock_value=NULL, locked_until=NULL, scheduled_at=$retry_at, attempt_count=attempt_count+1, last_error=$lastError` (research §5, §8) (depends on T010)
- [X] T021 [US2] In `05_ordered_tasks/pkg/taskworker/worker.go`, on `ProcessTask` returning a transient error and `attempt_count+1 < maxAttempts`: compute `retry_at = now + nextBackoffDelay(attemptCount)` from `--backoff-min`/`--backoff-max` exponential schedule and call `MarkFailedWithBackoff` (depends on T020, T015)
- [X] T022 [US2] Confirm the worker's eligibility scan correctly hides successors during backoff: in `worker.go`'s dedup-by-entity step, after picking the head per entity, drop the entity if `scheduled_at != nil && scheduled_at.After(now)`; do NOT consider any later seq for that entity in this iteration (research §4, §5) (depends on T015)
- [X] T023 [US2] Validate against quickstart §6: target-server `--fault-429-percent 30`; over ~60 s, worker logs show repeated `MarkFailedWithBackoff` for the same `task_id` then eventual success; target-server `violation_total == 0`; observed 429 rate within ±2 pp (depends on T021, T022, T036)

**Checkpoint**: User Story 2 fully functional — backoff windows do not violate per-entity ordering.

---

## Phase 5: User Story 3 — Permanent failure handling for an entity's head task (Priority: P2)

**Goal**: After retries exhaust, the head task transitions to `status='failed'`, the entity is observably blocked, and the operator-resolution path (`status='skipped'`) unblocks successors.

**Independent Test**: Force 100 % faults, `--max-attempts 3`, produce 5 tasks for one entity; verify head row reaches `failed` after 3 backoffs, successors remain `pending`, then issue the operator `UPDATE ... SET status='skipped'` and observe the next seq dispatched within one cycle (quickstart §7, SC-004, SC-005).

### Implementation for User Story 3

- [X] T024 [US3] Implement `repository_ydb.MarkTerminallyFailed(ctx, task, failedAt, lastError)` in `05_ordered_tasks/pkg/taskworker/repository_ydb.go` — serializable update conditioned on `status='locked' AND lock_value=$lv`, setting `status='failed', done_at=$failed_at, last_error=$lastError, lock_value=NULL, locked_until=NULL` (research §7, §8) (depends on T010)
- [X] T025 [US3] In `05_ordered_tasks/pkg/taskworker/worker.go`, on `ProcessTask` transient error with `attempt_count+1 >= maxAttempts`: call `MarkTerminallyFailed` instead of `MarkFailedWithBackoff` (depends on T024, T021)
- [X] T026 [US3] Confirm the eligibility scan never advances a `failed` entity: terminal states (`completed`, `failed`, `skipped`) are excluded from `FetchEligibleHeads`'s `status IN ('pending','locked')` predicate, so the failed head remains the smallest non-terminal seq for the entity and successors stay invisible (verification only — no code change beyond T012) (depends on T012)
- [X] T027 [US3] In `05_ordered_tasks/pkg/metrics/` (or worker shutdown stats block), surface a per-entity `blocked_reason` derived from `status` (`failed` → `terminal`, `pending && scheduled_at>now` → `backoff`) so operators can see blocked entities at process shutdown and via slog `entity_blocked` events emitted from the worker loop when the dedup step drops a head (FR-008, FR-012) (depends on T008, T015)
- [X] T028 [US3] Document the operator-resolution SQL recipe in `05_ordered_tasks/README.md`: `UPDATE ordered_tasks SET status='skipped', resolved_by=..., resolved_at=CurrentUtcTimestamp() WHERE entity_id=... AND status='failed'` (FR-009 audit fields) (depends on T003)
- [X] T029 [US3] Validate against quickstart §7: target-server at 100 % 429, worker `--max-attempts 3`, produce 5 tasks for one entity; confirm one row in `failed`, others still `pending`; apply the operator UPDATE; with fault injection off, the next seq dispatches within one cycle (depends on T025, T028, T036)

**Checkpoint**: User Story 3 fully functional — terminal failure is observable and operator-recoverable.

---

## Phase 6: User Story 4 — Test target HTTP server with order validation and fault injection (Priority: P1)

**Goal**: Single-file HTTP server that validates per-entity ordinal monotonicity on arrival, supports configurable HTTP 429 / 5xx fault injection, and exposes operator observability (`/state`, `/metrics`, `/healthz`) plus a shutdown stats block.

**Independent Test**: Start `06_target_server` standalone (no producer, no worker) and `curl` it directly: a sequence of `X-Entity-Seq` values for one entity that strictly increases produces 200s with `violation_total == 0`; sending a smaller seq emits a structured-log violation and increments the metric; configured fault rates produce 429/503 within ±2 pp tolerance (quickstart §2, SC-007, SC-008).

### Implementation for User Story 4

- [X] T030 [P] [US4] Implement HTTP ingest handler in `06_target_server/main.go` — `POST /` (catch-all): validate `Content-Type: application/json`, body ≤ 64 KiB else `413`, header `X-Entity-ID` non-empty else `400`, header `X-Entity-Seq` parseable non-zero `uint64` else `400` (contracts/target-server-ingest.md §Behaviour 1–3)
- [X] T031 [P] [US4] Implement fault-injection middleware in `06_target_server/main.go` — read `--fault-429-percent`, `--fault-5xx-percent`, validate sum ≤ 100 (else slog error and exit 1); per-request `roll := rand.Intn(100)`; `roll < fault429` → `429` body `{"status":"throttled","retry_after_ms":...}`; `roll < fault429+fault5xx` → `503`; otherwise fall through; increment `target_server_fault_injected_total{status=...}` (research §11, FR-018, FR-019)
- [X] T032 [P] [US4] Implement in-memory ordinal state in `06_target_server/main.go` — sharded `[]struct{mu sync.Mutex; m map[string]*entityState}` of length 64 keyed by `bucket = murmur3.Sum32(entity_id) % 64`; on accept (`recv > last`) update `LastAcceptedSeq`, `Accepted++`, `LastAcceptedAt`; on `recv == last` count duplicate; on `recv < last` increment `ordering_violation_total{bucket}` and slog.Warn `kind=rewind` carrying `entity_id, last_accepted_seq, received_seq, task_id`; respond `200` in all three cases (research §10, FR-016, FR-017) (depends on T030)
- [X] T033 [P] [US4] Implement `GET /healthz`, `GET /state` (with `?top=` default 50, max 1000, ordered by `LastAcceptedAt` desc), and `GET /metrics` (Prometheus text exposition with the series listed in contracts/target-server-observability.md) on a separate listener bound to `--metrics-port` (default `:9091`)
- [X] T034 [US4] Wire the entrypoint in `06_target_server/main.go`: `flag` parsing, `slog.NewJSONHandler` on stdout, two `http.Server`s (ingest on `--listen`, observability on `--metrics-port`), `signal.NotifyContext(SIGTERM, SIGINT)`, graceful shutdown via `srv.Shutdown(context.Background())`, then print plain stats block per contracts/target-server-observability.md §Stats block (depends on T030, T031, T032, T033)
- [X] T035 [US4] Add Prometheus histogram `target_server_request_duration_seconds` and gauge `target_server_fault_percent{kind=429|5xx}` to the `/metrics` exposition (depends on T033)
- [X] T036 [US4] Validate standalone per quickstart §2: run with no fault injection and `curl` the server with a hand-crafted strictly-increasing sequence of `X-Entity-Seq` for the same `X-Entity-ID`; observe `violation_total == 0`; then `curl` with a smaller seq and observe one violation log line and metric increment (depends on T034, T035)

**Checkpoint**: All four user stories independently functional. The target server is the dependency for the validation steps in T019, T023, and T029.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T037 [P] Run `go vet ./...` from repo root and confirm clean output (constitution build gate)
- [X] T038 [P] Run `go build -o /dev/null ./05_ordered_tasks/cmd/producer ./05_ordered_tasks/cmd/worker ./06_target_server` and confirm all three build with no warnings
- [X] T039 [P] Update `CLAUDE.md` "Active Technologies" / "Recent Changes" sections with feature 017 (no new direct go.mod deps; new table `ordered_tasks`; two new examples)
- [X] T040 Run the full quickstart.md flow §1–§8 against a live YDB instance; confirm SC-001, SC-002, SC-003, SC-004, SC-005, SC-007, SC-008, SC-009 all pass; capture the final stats blocks in the PR description

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 Setup**: no dependencies
- **Phase 2 Foundational**: depends on Phase 1; **blocks** all user stories
- **Phase 3 (US1)**: depends on Phase 2; T019 also depends on Phase 6 (T036)
- **Phase 4 (US2)**: depends on Phase 3 (worker loop must exist before failure path); T023 also depends on T036
- **Phase 5 (US3)**: depends on Phase 4 (terminal-failure path extends backoff path); T029 also depends on T036
- **Phase 6 (US4)**: depends on Phase 2 only — **fully independent** of US1/US2/US3 implementation, can start in parallel after Foundational
- **Phase 7 Polish**: depends on Phases 3–6

### Within Each User Story

- Repository methods (T010 first, then T012–T014 in parallel) before worker loop (T015)
- Worker loop (T015) before entrypoint (T016) and processor wiring (T018)
- Producer pkg (T011) before producer entrypoint (T017)
- US2's `MarkFailedWithBackoff` (T020) and US3's `MarkTerminallyFailed` (T024) only require `Candidate`/`ClaimedTask` types (T010), so they can start as soon as T010 lands

### Parallel Opportunities

- T001, T002, T003 in Phase 1 — different files, run in parallel
- T005, T006, T007, T008 in Phase 2 — independent package copies, run in parallel
- T010, T011 in Phase 3 — different packages, run in parallel
- T012, T013, T014, T020, T024 — all live in `repository_ydb.go`; sequential edits to the same file (NOT parallel)
- **Phases 3 and 6 can run in parallel by two developers**: worker/producer track and target-server track share no source files. The cross-phase dependency is only at validation time (T019, T023, T029 need T036).
- T030, T031, T032, T033 in Phase 6 — different concerns within `main.go`; if a single developer edits one file sequentially, treat as serial; if split across handlers in separate files, can be parallel
- T037, T038, T039 in Phase 7 — independent, parallel

---

## Parallel Example: Phase 2 Foundational

```bash
# After T004 lands, the four package copies are independent:
Task: "Copy ydbconn package to 05_ordered_tasks/pkg/ydbconn/"        # T005
Task: "Copy uid package to 05_ordered_tasks/pkg/uid/"                # T006
Task: "Copy rebalancer package to 05_ordered_tasks/pkg/rebalancer/"  # T007
Task: "Copy + adapt metrics package to 05_ordered_tasks/pkg/metrics/" # T008
```

## Parallel Example: Cross-Phase (US1 ∥ US4)

```bash
# After Phase 2 completes, two developers can split:
Developer A (US1, US2, US3): T010 → T011 → T012..T015 → T016..T018 → T020..T029
Developer B (US4):           T030 → T031 → T032 → T033 → T034 → T035 → T036
# Validation tasks T019/T023/T029 wait on T036 from Developer B.
```

---

## Implementation Strategy

### MVP First (US1 + US4)

US1 and US4 are both P1; the minimum demonstration of per-entity ordering requires **both** the worker and the validating target server. Recommended MVP scope:

1. Phase 1 Setup
2. Phase 2 Foundational (CRITICAL — blocks all stories)
3. Phase 3 US1 (producer + worker + head-of-entity dispatch) **in parallel with** Phase 6 US4 (target server)
4. Run quickstart §1–§5 → SC-001, SC-007 pass → MVP demo

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 + US4 → quickstart §1, §2, §5 → MVP (SC-001, SC-007)
3. US2 → quickstart §6 (SC-002, SC-003, SC-008, SC-009)
4. US3 → quickstart §7 (SC-004, SC-005)
5. Polish + full quickstart §8 → all SCs

### Format Validation

All 40 tasks above follow `- [ ] TXXX [P?] [Story?] Description with file path`. Story labels are present on US1/US2/US3/US4 tasks and absent from Setup/Foundational/Polish, matching the required format.

---

## Notes

- `[P]` tasks = different files, no dependencies on incomplete tasks
- The producer is single-instance per Clarifications; tasks do not include a multi-producer coordination path (out of scope for this fork)
- `priority` and `hash` columns are absent from this fork; do not add them anywhere
- `partition_id` is **always** derived from `entity_id` via `murmur3.Sum32` — never from `task_id` or anything else
- All status transitions out of `locked` are conditioned on `(status, lock_value)` to make at-least-once safe (FR-011)
- No new direct `go.mod` dependencies are required; all needed packages are already in `go.mod`
