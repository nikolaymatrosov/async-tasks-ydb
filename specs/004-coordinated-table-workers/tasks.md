# Tasks: Coordinated Table Workers

**Input**: Design documents from `/specs/004-coordinated-table-workers/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, quickstart.md

**Tests**: Not explicitly requested in the feature specification. Test tasks are omitted.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization — directory, entry point, CLI flags, YDB connection

- [X] T001 Create directory `04_coordinated_table/` and scaffold `04_coordinated_table/main.go` with package main, flag parsing (--endpoint, --database, --mode, --partitions, --coordination-path, --rate, --lock-duration, --backoff-min, --backoff-max), environment variable fallbacks (YDB_ENDPOINT, YDB_SA_KEY_FILE, YDB_ANONYMOUS_CREDENTIALS), signal.NotifyContext shutdown, and mode dispatch stub (producer vs worker)
- [X] T002 Implement YDB connection setup in `04_coordinated_table/main.go` — open YDB driver with ydb-go-yc auth, defer close, pass driver to mode functions

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Migration, coordination node creation, and shared types that ALL user stories depend on

**:warning: CRITICAL**: No user story work can begin until this phase is complete

- [X] T003 Create goose migration `migrations/20260329000005_create_coordinated_tasks.sql` with Up (CREATE TABLE coordinated_tasks with columns: id Utf8, hash Int64, partition_id Uint16, priority Uint8, status Utf8, payload Utf8, lock_value Utf8?, locked_until Timestamp?, scheduled_at Timestamp?, created_at Timestamp, done_at Timestamp?; PRIMARY KEY (partition_id, priority, id)) and Down (DROP TABLE)
- [X] T004 Add coordination node creation to `04_coordinated_table/main.go` — call `db.Coordination().CreateNode()` at startup for the configured coordination-path (idempotent, ignore "already exists" error)

**Checkpoint**: Foundation ready — migration and coordination node available for all stories

---

## Phase 3: User Story 1 - Producer fills tasks table (Priority: P1) :dart: MVP

**Goal**: A producer continuously inserts task rows into the `coordinated_tasks` table with hash-based partition routing and priority values.

**Independent Test**: Run `go run ./04_coordinated_table/ --mode producer` and verify rows appear in the table with valid hash values distributed across partitions 0-255.

### Implementation for User Story 1

- [X] T005 [US1] Implement producer in `04_coordinated_table/producer.go` — function that accepts context, YDB driver, rate, and partition count; in a ticker loop: generate UUID task ID, compute murmur3 hash, derive partition_id (hash % partitions), random priority 0-255, optional scheduled_at (see US4 — leave as nil for now), insert row with status "pending" via table client Upsert; respect context cancellation
- [X] T006 [US1] Wire producer mode in `04_coordinated_table/main.go` — when --mode=producer, call producer function with parsed flags; add slog output for producer start and periodic insert count

**Checkpoint**: Producer runs standalone, inserts tasks with correct hash/partition/priority distribution

---

## Phase 4: User Story 2 - Worker acquires partitions and processes tasks (Priority: P1)

**Goal**: A consumer worker acquires partition semaphores, polls for eligible tasks, locks them with optimistic CAS, simulates 100ms work, and marks them completed.

**Independent Test**: Start producer + single worker; verify worker acquires all 256 partitions, locks tasks (lock_value + locked_until set), and marks them completed (status=completed, done_at set).

### Implementation for User Story 2

- [X] T007 [US2] Implement partition acquisition in `04_coordinated_table/rebalancer.go` — Rebalancer struct holding: YDB driver, coordination session, worker ID (UUID), owned partitions map, target capacity, local weighted semaphore (golang.org/x/sync/semaphore); method to open coordination session, acquire worker-registry semaphore (shared, count=1), launch 256 goroutines each calling AcquireSemaphore(exclusive, ephemeral) for partition-N, use TryAcquire on local semaphore for capacity check, track leases; method returning channel of owned partition IDs; method for graceful release of all leases on shutdown
- [X] T008 [US2] Implement task polling and locking in `04_coordinated_table/worker.go` — Worker struct with: YDB driver, worker ID, lock duration, backoff config; method that given a partition ID runs a serializable read-write transaction: SELECT highest-priority eligible task (status=pending, scheduled_at IS NULL or <= now), UPDATE status=locked + lock_value=random UUID + locked_until=now+lock_duration; return locked task or nil
- [X] T009 [US2] Implement task processing loop in `04_coordinated_table/worker.go` — for each owned partition, run a goroutine that: polls for next task using T008 method, on success: spawn goroutine to sleep 100ms then UPDATE status=completed + done_at=now (in new transaction), reset backoff; on no task: apply exponential backoff (50ms initial, double, cap 5s); exit when partition lease context is done
- [X] T010 [US2] Wire worker mode in `04_coordinated_table/main.go` — when --mode=worker, create Rebalancer and Worker, start acquisition, launch processing goroutines for each acquired partition, handle shutdown (release leases, wait for in-flight tasks)
- [X] T011 [P] [US2] Implement stats display in `04_coordinated_table/display.go` — periodic (every 5s) slog + plain-text stats block showing: worker_id, partitions_owned count, tasks_processed count, tasks_locked count, uptime; collect stats via atomic counters or mutex-guarded struct shared with worker

**Checkpoint**: Single worker acquires all 256 partitions, processes tasks in priority order, displays stats

---

## Phase 5: User Story 3 - Dynamic partition rebalancing across workers (Priority: P1)

**Goal**: When workers join or leave, partitions are automatically redistributed so each handles a roughly equal share.

**Independent Test**: Start 2 workers — each should hold ~128 partitions. Start a 3rd — each should hold ~85. Kill one — remaining 2 rebalance to ~128 each.

### Implementation for User Story 3

- [X] T012 [US3] Implement membership watch in `04_coordinated_table/rebalancer.go` — method that calls DescribeSemaphore on worker-registry with WatchOwners=true in a loop; on owner count change: recalculate target_capacity = ceil(256 / active_workers); if currently holding more than target, release excess leases (release those acquired most recently); if below target, resume acquisition goroutines for unowned partitions; emit slog rebalancing event with old_count, new_count, reason
- [X] T013 [US3] Integrate rebalancing with worker processing in `04_coordinated_table/worker.go` — when a partition lease is released by rebalancer, cancel the partition's processing goroutine (lease.Context() done); when a new partition is acquired, start a new processing goroutine for it; ensure no race between release and in-flight task completion (wait for goroutine exit before releasing lease)

**Checkpoint**: Multiple workers dynamically share 256 partitions; rebalancing completes within 10s of membership change

---

## Phase 6: User Story 4 - Priority-based task processing and postponed tasks (Priority: P2)

**Goal**: Producer creates tasks with varying priorities and optional scheduled_at. Workers always pick highest-priority eligible task first and skip postponed tasks.

**Independent Test**: Insert tasks with different priorities and scheduled_at values into one partition; verify worker processes them in priority order and skips future-scheduled tasks.

### Implementation for User Story 4

- [X] T014 [US4] Update producer in `04_coordinated_table/producer.go` to occasionally set scheduled_at to a future timestamp (e.g., 10% of tasks get scheduled_at = now + random 5-30s) for demonstrating postpone behavior
- [X] T015 [US4] Update task polling query in `04_coordinated_table/worker.go` to add `AND (scheduled_at IS NULL OR scheduled_at <= CurrentUtcTimestamp())` filter and `ORDER BY priority DESC` to the eligible-task SELECT (this should already be partially in place from T008 — verify and complete)

**Checkpoint**: Tasks processed strictly by priority; postponed tasks skipped until eligible

---

## Phase 7: User Story 5 - Stale lock recovery (Priority: P2)

**Goal**: Workers reclaim tasks whose locked_until has expired, ensuring no task is permanently stuck.

**Independent Test**: Lock a task with a short locked_until, let it expire, verify another worker reclaims it.

### Implementation for User Story 5

- [X] T016 [US5] Update task polling query in `04_coordinated_table/worker.go` to include stale-lock recovery: extend the WHERE clause to also match `(status = 'locked' AND locked_until < CurrentUtcTimestamp())` — this makes expired-lock tasks eligible alongside pending tasks (this should already be partially in place from T008 — verify, complete, and add slog "task reclaimed" event with old lock_value for observability)

**Checkpoint**: Expired locks are automatically reclaimed; no stuck tasks

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Structured logging, graceful shutdown, and final validation

- [X] T017 [P] Ensure all slog output in `04_coordinated_table/` uses JSON handler with structured fields per quickstart.md expectations (worker_id, partition_id, task_id, priority, reason, old_count, new_count)
- [X] T018 [P] Verify graceful shutdown in `04_coordinated_table/main.go` — on SIGINT/SIGTERM: producer stops inserting, workers finish in-flight tasks, release all leases, close coordination session, close YDB driver; no goroutine leaks
- [X] T019 Validate end-to-end by running quickstart.md steps mentally against the implementation — confirm all CLI flags, env vars, slog output format, and stats block match quickstart.md expectations
- [X] T020 Run `go vet ./04_coordinated_table/` and fix any issues

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion — BLOCKS all user stories
- **US1 Producer (Phase 3)**: Depends on Phase 2
- **US2 Worker (Phase 4)**: Depends on Phase 2 (and benefits from US1 for testing, but not a code dependency)
- **US3 Rebalancing (Phase 5)**: Depends on Phase 4 (extends rebalancer.go and worker.go from US2)
- **US4 Priority/Postpone (Phase 6)**: Depends on Phase 3 (producer) + Phase 4 (worker query)
- **US5 Stale Lock (Phase 7)**: Depends on Phase 4 (worker query)
- **Polish (Phase 8)**: Depends on all user stories being complete

### User Story Dependencies

- **US1 (P1 - Producer)**: After Phase 2 — no dependencies on other stories
- **US2 (P1 - Worker)**: After Phase 2 — no code dependency on US1, but needs tasks in table to test
- **US3 (P1 - Rebalancing)**: After US2 — extends rebalancer.go with membership watch
- **US4 (P2 - Priority/Postpone)**: After US1 + US2 — modifies producer.go and worker.go
- **US5 (P2 - Stale Lock)**: After US2 — modifies worker.go query

### Within Each User Story

- Models/schema before business logic
- Core implementation before integration wiring
- Story complete before moving to next priority

### Parallel Opportunities

- **Phase 1**: T001 and T002 are sequential (T002 builds on T001)
- **Phase 2**: T003 and T004 can run in parallel [P]
- **Phase 3 (US1)**: T005 → T006 sequential
- **Phase 4 (US2)**: T007, T008, T011 can run in parallel [P]; T009 depends on T008; T010 depends on T007+T009
- **Phase 5 (US3)**: T012 → T013 sequential (T013 integrates T012's output)
- **Phase 6 (US4)**: T014 and T015 can run in parallel [P]
- **Phase 7 (US5)**: T016 is a single task
- **Phase 8**: T017, T018 can run in parallel [P]; T019 depends on all prior; T020 last

---

## Parallel Example: User Story 2 (Worker)

```bash
# Launch in parallel — different files, no dependencies:
Task T007: "Implement partition acquisition in 04_coordinated_table/rebalancer.go"
Task T008: "Implement task polling and locking in 04_coordinated_table/worker.go"
Task T011: "Implement stats display in 04_coordinated_table/display.go"

# Then sequentially:
Task T009: "Implement task processing loop in 04_coordinated_table/worker.go" (needs T008)
Task T010: "Wire worker mode in 04_coordinated_table/main.go" (needs T007 + T009)
```

---

## Implementation Strategy

### MVP First (US1 + US2 Only)

1. Complete Phase 1: Setup (main.go scaffold + YDB connection)
2. Complete Phase 2: Foundational (migration + coordination node)
3. Complete Phase 3: US1 Producer (insert tasks)
4. Complete Phase 4: US2 Worker (acquire partitions, process tasks)
5. **STOP and VALIDATE**: Single worker processes all tasks from producer
6. Demo-ready with single-worker behavior

### Incremental Delivery

1. Setup + Foundational -> Foundation ready
2. Add US1 Producer -> Tasks flowing into table
3. Add US2 Worker -> Single worker processing (MVP!)
4. Add US3 Rebalancing -> Multi-worker distribution (key demo feature)
5. Add US4 Priority/Postpone -> Realistic scheduling semantics
6. Add US5 Stale Lock -> Fault tolerance
7. Polish -> Production-quality logging and shutdown

### Suggested MVP Scope

**US1 (Producer) + US2 (Worker)**: Phases 1-4, tasks T001-T011. This delivers a working producer-consumer system with partition-based distribution. Rebalancing (US3) is the natural next increment since it's P1 and the key distributed systems concept.

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- All files are `package main` in `04_coordinated_table/` — `go run ./04_coordinated_table/` works
- `golang.org/x/sync/semaphore` may need promotion from indirect to direct dependency (R6)
