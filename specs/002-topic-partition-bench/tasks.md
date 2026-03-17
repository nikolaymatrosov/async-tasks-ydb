# Tasks: Topic Partition Benchmark

**Input**: Design documents from `/specs/002-topic-partition-bench/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/cli-flags.md ✅, quickstart.md ✅

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US4)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish database schema before any Go code is written

- [X] T001 Create `migrations/20260316000004_create_bench_infra.sql` — goose Up creates `tasks/by_user` topic (10 partitions, 24h retention, consumers `bench-byuser-stats` and `bench-byuser-processed`), `tasks/by_message_id` topic (10 partitions, 24h retention, consumers `bench-bymsgid-stats` and `bench-bymsgid-processed`), `stats` table (user_id UUID PK, a Int64, b Int64, c Int64), `processed` table (id UUID PK); Down drops all four objects in reverse order

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core data types and utilities required by all other files

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T002 Create `03_topic/message.go` — define `BenchMessage` struct (`ID uuid.UUID`, `UserID uuid.UUID`, `Type string`) with JSON field tags (`id`, `user_id`, `type`) and `ScenarioResult` struct (`Name string`, `Messages int64`, `TLIErrors int64`, `Duration time.Duration`, `MsgPerSec float64`); `MsgPerSec` is computed, not stored (package main, no sub-packages)
- [X] T003 [P] Confirm `03_topic/utils.go` and `03_topic/utils_test.go` are unchanged — read both files and verify `UserIDSampler` exposes `Sample() string`, `IDs() []string`, and `All() iter.Seq[Entry]`; make no edits

**Checkpoint**: Foundation ready — user story implementation can now begin

---

## Phase 3: User Story 1 — End-to-End Benchmark Orchestration (Priority: P1) 🎯 MVP

**Goal**: A runnable binary that generates messages, publishes to both topics, executes all 4 scenarios with stub workloads, and prints the Unicode comparison table.

**Independent Test**: Run `go run ./03_topic/` (with env vars set and migration applied) and confirm a 4-row comparison table is printed to stdout matching the format in `contracts/cli-flags.md`.

- [X] T004 [P] [US1] Create `03_topic/producer.go` — implement `hashKey(key string, partitions int) int64` using `github.com/spaolacci/murmur3` (absolute value modulo partitions); `safeWriter` struct wrapping `*topicoptions.Writer` with `sync.Mutex` for concurrent writes; `Producer` struct with `db *ydb.Driver` field; `NewProducer(db *ydb.Driver) *Producer`; `Generate(n, users int, sampler *UserIDSampler) []BenchMessage` that creates n `BenchMessage` values with `uuid.New()` IDs, sampled UserIDs, and round-robin Type cycling through "A"/"B"/"C"; `Publish(ctx context.Context, messages []BenchMessage, topicPath string, keyFn func(BenchMessage) string) error` that writes each message as JSON to the partition determined by `hashKey(keyFn(msg), 10)`, flushing the writer on completion
- [X] T005 [P] [US1] Create `03_topic/consumer.go` — implement `Consumer` struct (`db *ydb.Driver`); `RunScenario(ctx context.Context, topic, consumerName string, partitionCount int, target int64, workload func(context.Context, BenchMessage) error) (ScenarioResult, error)` that: creates a `sync/atomic.Int64` message counter and a TLI counter; launches `partitionCount` goroutines each calling `db.Topic().StartReader(consumerName, topicoptions.ReadSelectors{{Path: topic, Partitions: []int64{int64(i)}}})`, reads batches, deserializes JSON into `BenchMessage`, calls workload, increments the message counter, cancels the scenario context when counter reaches target; collects results and returns `ScenarioResult` with wall-clock duration computed via `time.Since(start)` and `MsgPerSec` derived from Messages/Duration.Seconds()
- [X] T006 [US1] Rewrite `03_topic/main.go` — hardcoded defaults for now (users=100, messages=100000, topicUser="tasks/by_user", topicID="tasks/by_message_id"); validate `YDB_ENDPOINT` and `YDB_SA_KEY_FILE` env vars with `slog.Error` + `os.Exit(1)` if missing; `signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)` for graceful shutdown; connect YDB via `ydb.Open` with `yc.WithServiceAccountKeyFileCredentials`; instantiate `Producer`, call `Generate`, call `Publish` for both topics; run 4 scenarios sequentially using `Consumer.RunScenario` with a placeholder `noopWorkload` function that always returns nil; collect `[]ScenarioResult`; print the Unicode box-drawing comparison table to stdout with columns `Scenario`, `Messages`, `TLI Errors`, `Duration`, `msg/sec` matching the format in `contracts/cli-flags.md`

**Checkpoint**: `go run ./03_topic/` compiles and prints a 4-row table (TLI errors all 0 at this stage since noop workload is used)

---

## Phase 4: User Story 2 — RMW Stats Workload with TLI Counting (Priority: P1)

**Goal**: Replace noop workloads for stats scenarios with the real read-modify-write transaction logic that counts TLI errors, demonstrating ≥10× fewer errors for user-aligned partitioning.

**Independent Test**: Run the benchmark and confirm `by_user → stats` TLI count is at least 10× lower than `by_message_id → stats` TLI count.

- [X] T007 [US2] Add stats workload in `03_topic/consumer.go` — implement `statsWorkload(db *ydb.Driver, tliCounter *atomic.Int64) func(context.Context, BenchMessage) error`: inside the returned function use `db.Query().Do(ctx, func(ctx context.Context, s query.Session) error { ... })` with `s.Begin(ctx, query.TxSettings(query.WithSerializableReadWrite()))`, execute `SELECT a, b, c FROM stats WHERE user_id = $uid`, increment the counter for `msg.Type` ("A"→a, "B"→b, "C"→c) treating NULL as 0, execute `UPSERT INTO stats (user_id, a, b, c) VALUES ($uid, $a, $b, $c)`, call `tx.CommitTx(ctx)`, and — at the CommitTx error check — call `tliCounter.Add(1)` before `return err` when `ydb.IsOperationErrorTransactionLocksInvalidated(err)` is true, allowing `Do` to retry; wire this workload into scenarios 1 and 3 in `main.go` replacing the noop
- [X] T008 [US2] Add stats verification and reset in `03_topic/main.go` — after each stats scenario completes, query `SELECT SUM(a) + SUM(b) + SUM(c) AS total FROM stats` via `db.Query().QueryRow` and emit `slog.Warn` with field `expected` and `actual` if total != target messages (SC-004); before starting scenario 3 (by_message_id → stats), execute `db.Query().Exec(ctx, "DELETE FROM stats")` to reset the table (FR-006)

**Checkpoint**: Benchmark shows realistic TLI counts; `by_user → stats` TLI substantially lower than `by_message_id → stats`

---

## Phase 5: User Story 3 — Insert-Only Processed Workload (Priority: P2)

**Goal**: Replace noop workloads for processed scenarios with the real insert-only logic, demonstrating zero TLI regardless of partition key.

**Independent Test**: Run the benchmark and confirm both `→ processed` rows show 0 TLI errors.

- [X] T009 [US3] Add processed workload in `03_topic/consumer.go` — implement `processedWorkload(db *ydb.Driver) func(context.Context, BenchMessage) error`: inside the returned function call `db.Query().Exec(ctx, "UPSERT INTO processed (id) VALUES ($id)", query.Parameter("$id", types.UUIDValue(msg.ID)))` — no preceding SELECT, no TLI counter; wire this workload into scenarios 2 and 4 in `main.go` replacing the noop

**Checkpoint**: Both processed scenarios report 0 TLI errors; insert-only vs RMW contrast is visible in the table

---

## Phase 6: User Story 4 — CLI Configuration Flags (Priority: P3)

**Goal**: Accept runtime parameters so the benchmark can be tuned without recompiling.

**Independent Test**: Run `go run ./03_topic/ -users 50 -messages 10000` and confirm all 4 scenario rows show `Messages = 10000`.

- [X] T010 [US4] Add flag parsing in `03_topic/main.go` — replace hardcoded defaults with `flag.Int("users", 100, "number of distinct user IDs")`, `flag.Int("messages", 100000, "total messages per topic")`, `flag.String("topic-user", "tasks/by_user", "user-partitioned topic path")`, `flag.String("topic-id", "tasks/by_message_id", "message-ID-partitioned topic path")`; call `flag.Parse()` before env-var validation; add guards: if `*users < 1` or `*messages < 1`, emit `slog.Error` with the invalid value and call `os.Exit(1)` (contracts/cli-flags.md validation rules)

**Checkpoint**: Custom flag values are reflected in output; invalid flags produce slog.Error and exit code 1

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Finalize logging contract, shutdown guarantees, and end-to-end validation

- [X] T011 [P] Add structured slog events in `03_topic/producer.go` and `03_topic/consumer.go` — emit `slog.Info("producer started", "topic", topicPath, "partitions", 10)` before publish loop; `slog.Info("publish complete", "topic", topicPath, "messages", len(messages))` after; `slog.Info("scenario started", "scenario", name)` at the top of `RunScenario`; `slog.Info("scenario complete", "scenario", name, "messages", result.Messages, "tli_errors", result.TLIErrors, "duration_s", result.Duration.Seconds())` on return — matching the JSON format defined in `contracts/cli-flags.md`
- [X] T012 Verify graceful shutdown in `03_topic/main.go` — confirm that `signal.NotifyContext` cancellation propagates through all `RunScenario` goroutine contexts; if context is cancelled before all 4 scenarios complete, skip printing the comparison table and return a non-zero exit code (contracts/cli-flags.md exit codes)
- [ ] T013 [P] Run end-to-end validation following `quickstart.md` — requires live YDB instance — apply migration, run `go run ./03_topic/` with default flags, confirm 4-row comparison table is printed; verify `by_user → stats` TLI is ≥10× lower than `by_message_id → stats` (SC-001); confirm both processed rows show 0 TLI (SC-002); confirm all Messages columns equal 100000 (SC-003)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Phase 2 — implements the full pipeline skeleton
- **US2 (Phase 4)**: Depends on Phase 3 — replaces noop with real stats RMW workload
- **US3 (Phase 5)**: Depends on Phase 3 — replaces noop with real processed insert workload; can run in parallel with US2
- **US4 (Phase 6)**: Depends on Phase 3 — adds flag parsing on top of working binary
- **Polish (Phase 7)**: Depends on Phases 4, 5, 6 all complete

### User Story Dependencies

- **US1 (P1)**: Can start after Phase 2 — no dependencies on other stories
- **US2 (P1)**: Depends on US1 (Phase 3) being complete
- **US3 (P2)**: Depends on US1 (Phase 3) being complete — can run in parallel with US2
- **US4 (P3)**: Depends on US1 (Phase 3) being complete — can run in parallel with US2/US3

### Parallel Opportunities

- T004 (producer.go) and T005 (consumer.go) can be implemented in parallel (different files)
- T003 (verify utils.go) can run in parallel with T002 (message.go)
- US2 (T007–T008) and US3 (T009) can be implemented in parallel (different code paths in consumer.go — note: both touch consumer.go so coordinate on merging)
- US4 (T010) can be implemented in parallel with US2 and US3
- T011 (logging) can be done in parallel with T012 (shutdown) and T013 (validation)

---

## Parallel Example: User Story 1 (Phase 3)

```bash
# T004 and T005 can be worked on simultaneously (different files):
Task: "Create 03_topic/producer.go with hashKey, safeWriter, Producer, Generate, Publish"
Task: "Create 03_topic/consumer.go with Consumer struct and RunScenario"

# T006 (main.go rewrite) must wait for T004 and T005 to be complete
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Migration file
2. Complete Phase 2: message.go + confirm utils.go
3. Complete Phase 3: producer.go, consumer.go skeleton, main.go rewrite
4. **STOP and VALIDATE**: `go run ./03_topic/` prints a 4-row table with noop workloads
5. Demo: pipeline is wired end-to-end

### Incremental Delivery

1. Setup + Foundational → schema and data types in place
2. US1 (Phase 3) → `go run ./03_topic/` works, table prints (noop TLI = 0)
3. US2 (Phase 4) → stats workload live, TLI contrast visible
4. US3 (Phase 5) → processed workload live, contention-free baseline confirmed
5. US4 (Phase 6) → configurable via flags
6. Polish (Phase 7) → logging, shutdown, final validation

---

## Notes

- No new `go.mod` direct dependencies — all required packages (`murmur3`, `uuid`, `ydb-go-sdk/v3`, `ydb-go-yc`) are already in `go.mod`
- All files in `03_topic/` are `package main`; `go run ./03_topic/` compiles all `.go` files together
- `utils.go` and `utils_test.go` are explicitly marked KEEP AS-IS in plan.md — do not modify them
- TLI detection must happen *inside* the `Do` closure, before returning the error, so the SDK's retry can fire and the count captures every invalidation event (research.md Decision 1)
- Per-partition readers (one goroutine per partition ID) are required for the experiment to be valid (research.md Decision 2)
- Commit after each task or logical group; stop at any checkpoint to validate independently
