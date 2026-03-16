# Tasks: Example 03 — Direct Topic Writer

**Input**: Design documents from `/specs/001-03-topic-writer/`
**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, quickstart.md ✓

**Organization**: Tasks are grouped by user story to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the `03_topic/` directory and the payload struct — the minimum skeleton before any story work begins.

- [X] T001 Create `03_topic/` directory at repo root with empty `main.go` (`package main`) and `README.md` stub
- [X] T002 [P] Define `TaskMessage` struct in `03_topic/main.go` with fields: `ID string` (UUID), `Payload []byte` (random), `CreatedAt time.Time`; add `generateMessage()` helper that populates all fields randomly and returns the struct JSON-encoded as `[]byte`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: YDB connection setup and `Producer` / `safeWriter` types — must be complete before any user story can function.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T003 Implement `main()` bootstrap in `03_topic/main.go`: parse `YDB_ENDPOINT` and `YDB_SA_KEY_FILE` env vars (fatal if missing), open `ydb.Driver` with `yc.WithServiceAccountKeyFileCredentials` and `yc.WithInternalCA()`, defer `db.Close`, set up `signal.NotifyContext` for graceful shutdown, initialise `slog.Default()` JSON handler
- [X] T004 Declare `Producer` struct in `03_topic/main.go` with fields: `db *ydb.Driver`, `topic string`, `partitions []int64`, `writers map[int64]*safeWriter`; declare `safeWriter` struct with field `w *topicwriter.Writer`
- [X] T005 Add `-topic` flag (default `tasks/direct`) to `main()` in `03_topic/main.go`; construct full absolute topic path as `db.Name() + "/" + topicFlag`; pass it to `NewProducer`
- [X] T006 Implement `NewProducer(db *ydb.Driver, topicPath string) *Producer` in `03_topic/main.go`: set `db` and `topic` fields; leave `partitions` and `writers` nil

**Checkpoint**: Foundation ready — `Producer` struct exists, YDB driver opens, topic path is constructed.

---

## Phase 3: User Story 1 — Consistent Partition Routing (Priority: P1) 🎯 MVP

**Goal**: `Producer.Start()` enumerates active partitions, opens one pinned writer per partition, and `Producer.Write()` routes messages deterministically by murmur3 hash of the partition key.

**Independent Test**: Run `go run ./03_topic/` with two partition keys; observe in slog output that all messages for key A go to the same partition ID and all messages for key B go to a different (consistent) partition ID.

### Implementation for User Story 1

- [X] T007 [US1] Implement `(p *Producer) Start(ctx context.Context) error` in `03_topic/main.go`: panic if `p.writers != nil`; call `p.db.Topic().Describe(ctx, p.topic)` to get active partitions; for each active partition call `p.db.Topic().StartWriter(p.topic, topicoptions.WithWriterPartitionID(id), topicoptions.WithWriterWaitServerAck(true))`; populate `p.partitions` slice and `p.writers` map; return error (with partial cleanup via `p.Stop`) on any writer init failure
- [X] T008 [US1] Implement `hashKey(key string) uint32` function in `03_topic/main.go` using `murmur3.Sum32([]byte(key))` from `github.com/twmb/murmur3`; use result modulo `len(p.partitions)` as index into `p.partitions` slice inside `Producer.Write`
- [X] T009 [US1] Implement `(p *Producer) Write(ctx context.Context, partitionKey string, messages ...topicwriter.Message) error` in `03_topic/main.go`: return error if `p.writers == nil`; compute `partitionID` via `hashKey`; delegate to `p.writers[partitionID].Write(ctx, messages)`
- [X] T010 [US1] Wire demo loop in `main()` in `03_topic/main.go`: define two partition keys (e.g. `"user-42"`, `"order-99"`); loop 5 messages per key; for each call `generateMessage()`, marshal to JSON, create `topicwriter.Message{Data: bytes.NewReader(payload)}`, call `producer.Write`; log each delivery with `slog.Info` including `partition_key`, `partition_id`, and `msg_index` fields

**Checkpoint**: US1 complete — run `go run ./03_topic/`; confirm consistent partition IDs per key in slog output.

---

## Phase 4: User Story 2 — Resilient Writes with Automatic Retry (Priority: P2)

**Goal**: `safeWriter.Write` retries on transport errors with exponential backoff (max 30 s interval, 5 min total), retries indefinitely on `ErrQueueLimitExceed`, and surfaces permanent errors immediately.

**Independent Test**: Temporarily break the YDB endpoint after `Start()`; observe slog retry warnings; restore endpoint; confirm writes eventually succeed within 5 minutes.

### Implementation for User Story 2

- [X] T011 [US2] Implement `(w *safeWriter) Write(ctx context.Context, messages []topicwriter.Message) error` in `03_topic/main.go` using `backoff.NewExponentialBackOff()` from `github.com/cenkalti/backoff/v4`: set `MaxInterval = 30*time.Second`, `MaxElapsedTime = 5*time.Minute`; wrap in `backoff.WithContext`; call `backoff.Retry` with inner function that calls `w.w.Write(ctx, messages...)`
- [X] T012 [US2] Add error classification inside the `backoff.Retry` callback in `03_topic/main.go`: if `ctx.Err() != nil` return `backoff.Permanent(ctx.Err())`; if `errors.Is(err, topicwriter.ErrQueueLimitExceed)` reset backoff elapsed time and return the error as-is (triggers unlimited retry); for all other errors check `ydb.IsTransportError(err)` — if true return the error (retryable); otherwise return `backoff.Permanent(err)`
- [X] T013 [US2] Add retry warning logging in `03_topic/main.go`: inside the `backoff.Retry` notify function (fourth argument) log with `slog.Warn` including `err`, `retry_in`, `partition_id` fields so retry behaviour is visible in output

**Checkpoint**: US2 complete — retry logic in place; transport errors are retried; permanent errors surface immediately.

---

## Phase 5: User Story 3 — Graceful Startup and Shutdown (Priority: P3)

**Goal**: `Producer.Stop()` closes all partition writers cleanly (joining errors), and the `main()` loop calls `Stop` on signal/exit.

**Independent Test**: Run the example to completion (it exits after the demo loop); confirm slog output shows "producer stopped" and no goroutine leak is reported.

### Implementation for User Story 3

- [X] T014 [US3] Implement `(p *Producer) Stop(ctx context.Context) error` in `03_topic/main.go`: iterate `p.writers`, call `w.w.Close(ctx)` for each, collect errors into a slice, delete each key, join and return all errors; log each close error with `slog.Error`
- [X] T015 [US3] Wire shutdown in `main()` in `03_topic/main.go`: defer `producer.Stop(context.Background())` immediately after a successful `producer.Start()`; log start and stop events with `slog.Info`
- [X] T016 [US3] Add double-init guard in `(p *Producer) Start` in `03_topic/main.go`: if `p.writers != nil` panic with message `"producer already started"`; add slog info log at end of `Start` listing partition count

**Checkpoint**: US3 complete — full lifecycle visible in logs; clean shutdown on Ctrl-C or demo loop completion.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: README, final stats output, and consistency pass.

- [X] T017 [P] Write `03_topic/README.md`: one-time `ydb topic create` setup command, env var table (`YDB_ENDPOINT`, `YDB_SA_KEY_FILE`), run command (`go run ./03_topic/`), `-topic` and `-messages` flags, expected slog output example, troubleshooting table — mirror style of `01_db_producer/README.md`
- [X] T018 Add final stats block to `main()` in `03_topic/main.go`: after demo loop and before `Stop`, print to stdout: total messages written, keys used, unique partition IDs observed — mirrors stats footer in existing examples
- [X] T019 Update `03_topic/plan.md` constitution check note: replace "no new dependencies" row with updated row acknowledging `cenkalti/backoff/v4` and `twmb/murmur3` are now in `go.mod`
- [X] T020 Run `go build ./03_topic/` and resolve any compilation errors in `03_topic/main.go`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 — blocks all user stories
- **US1 (Phase 3)**: Depends on Phase 2 — no dependency on US2/US3
- **US2 (Phase 4)**: Depends on Phase 2 — modifies `safeWriter` only, no dependency on US1 completion
- **US3 (Phase 5)**: Depends on Phase 2 — modifies `Stop` and `main()`, no dependency on US1/US2 completion
- **Polish (Phase 6)**: Depends on all user stories complete

### User Story Dependencies

- **US1**: Independent after Phase 2
- **US2**: Independent after Phase 2; enhances `safeWriter.Write` (different function from US1's `Producer.Write`)
- **US3**: Independent after Phase 2; enhances `Producer.Stop` and lifecycle wiring

### Parallel Opportunities

- T002 (payload struct) can run in parallel with T003–T006 (different concern, same file section)
- T007, T008, T009 (US1) are sequential within US1
- T011, T012, T013 (US2) are sequential within US2 but the whole US2 phase can run in parallel with US1
- T014, T015, T016 (US3) are sequential within US3 but US3 phase can run in parallel with US1 and US2
- T017 (README) and T018 (stats) can run in parallel in Phase 6

---

## Parallel Example: US1 + US2 + US3

```bash
# After Phase 2 completes, these three tracks can proceed in parallel:

Track A (US1 - Routing):
  T007 → T008 → T009 → T010

Track B (US2 - Retry):
  T011 → T012 → T013

Track C (US3 - Lifecycle):
  T014 → T015 → T016
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001–T002)
2. Complete Phase 2: Foundational (T003–T006)
3. Complete Phase 3: US1 (T007–T010)
4. **STOP and VALIDATE**: `go run ./03_topic/` — confirm consistent partition IDs in slog output
5. Demo if ready

### Incremental Delivery

1. Phase 1 + 2 → skeleton compiles
2. Phase 3 (US1) → routing works end-to-end ✓ MVP
3. Phase 4 (US2) → retry logic added, resilient writes ✓
4. Phase 5 (US3) → clean lifecycle, shutdown ✓
5. Phase 6 → README, stats, polish ✓

---

## Notes

- All logic lives in `03_topic/main.go` — no sub-packages (single-file example convention)
- No test files generated — not requested in spec; manual end-to-end run is the validation method
- `safeWriter.Write` signature takes `[]topicwriter.Message` (slice), while `Producer.Write` is variadic and converts `...topicwriter.Message` to slice before delegating
- The `ErrQueueLimitExceed` infinite-retry is achieved by resetting backoff inside the notify function or by wrapping in an outer `for{}` loop — choose the approach that reads most clearly
- `[P]` tasks operate on distinct logical sections of `main.go` or distinct files; concurrent edits to the same file still require sequencing in practice
