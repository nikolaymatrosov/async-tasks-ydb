# Tasks: Batch Producer Rate Control

**Input**: Design documents from `/specs/008-batch-producer-rate/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, quickstart.md ✅

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.
**Tests**: Not requested — manual end-to-end verification per quickstart.md.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- No tests generated (not requested in spec)

---

## Phase 1: Setup (CLI Flags)

**Purpose**: Expose the two new flags so all subsequent wiring has concrete values to reference.

- [X] T001 Add `--batch-window` (default `100ms`, type `time.Duration`) and `--report-interval` (default `5s`, type `time.Duration`) CLI flags to `04_coordinated_table/main.go` alongside the existing `--rate` flag; store in a local config struct or pass directly to the producer constructor

**Checkpoint**: `go run ./04_coordinated_table/ --help` shows both new flags.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Producer metrics infrastructure and generalised metrics handler — both must exist before the batch loop can record anything.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T002 [P] Create `04_coordinated_table/producer_stats.go` with `ProducerStats` struct (fields: `registry *prometheus.Registry`, `startTime time.Time`, plus all metric handles) and `newProducerStats(targetRate float64, window time.Duration) *ProducerStats` constructor that registers these metrics on a fresh `prometheus.NewRegistry()`: `producer_up` (Gauge), `producer_target_rate` (Gauge), `producer_window_seconds` (Gauge), `producer_target_batch_size` (Gauge), `producer_inserted_total` (Counter), `producer_batches_total` (Counter), `producer_batch_errors_total` (Counter), `producer_backpressure_total` (Counter), `producer_observed_rate` (Gauge), `producer_batch_size` (Histogram, buckets `1,10,100,1000,10000`), `producer_batch_duration_seconds` (Histogram, buckets `0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2,5`); also register `collectors.NewGoCollector()` and `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})` to mirror worker behaviour
- [X] T003 [P] Edit `04_coordinated_table/metrics.go` to change `metricsHandler` signature from `metricsHandler(stats *Stats)` to `metricsHandler(registry *prometheus.Registry)` and update the gather/handler body to call `registry.Gather()` (or use `promhttp.HandlerFor(registry, ...)`) instead of the Stats-specific registry; update the existing worker-mode call site in `main.go` accordingly so `Stats.registry` is passed directly
- [X] T004 Edit `04_coordinated_table/main.go` to branch on `--mode`: when `mode == "producer"`, create `ps := newProducerStats(float64(rate), batchWindow)` and call `go http.ListenAndServe(addr, metricsHandler(ps.registry))`; when `mode == "worker"`, create `stats := newStats(workerID)` as before and call `go http.ListenAndServe(addr, metricsHandler(stats.registry))`; set `ps.up.Set(1)` at startup and `ps.up.Set(0)` on shutdown (defer)

**Checkpoint**: `go vet ./...` passes; `/metrics` in producer mode shows `producer_up 1` with no worker counters.

---

## Phase 3: User Story 1 — Precise Rate at Any Scale (Priority: P1) 🎯 MVP

**Goal**: Replace the per-task ticker loop with a fixed-window batch loop that assembles `rate * window` rows per window and submits them as a single YDB `UPSERT INTO coordinated_tasks SELECT ... FROM AS_TABLE($records)`.

**Independent Test**: Run `go run ./04_coordinated_table/ --mode producer --rate 100 --batch-window 100ms` for 60 s. `SELECT COUNT(*) FROM coordinated_tasks` must return between 5,700 and 6,300.

- [X] T005 [US1] Define `taskRow` struct in `04_coordinated_table/producer.go` with fields `id string`, `hash int64`, `partitionID uint16`, `priority uint8`, `payload string`, `createdAt time.Time`, `scheduledAt *time.Time`; implement `buildBatch(ctx context.Context, batchSize int, partitions int) []taskRow` that appends `batchSize` rows using the existing per-task attribute logic (uuid for id, murmur3 hash, partition mod, uniform random priority, `status="pending"`, per-row `createdAt=time.Now().UTC()`, ~10% scheduled_at 5–30 s ahead) — stops early and returns a partial slice if `ctx.Done()` fires during assembly
- [X] T006 [US1] Implement `upsertBatch(ctx context.Context, db *ydb.Driver, batch []taskRow) error` in `04_coordinated_table/producer.go` using `db.Query().Exec(ctx, query, params)` where `query` is `UPSERT INTO coordinated_tasks SELECT id, hash, partition_id, priority, "pending" AS status, payload, created_at, scheduled_at FROM AS_TABLE($records)` and `$records` is built with `types.ListValue(...)` of `types.StructValue` entries per row; use `types.NullableTimestampValue` for `scheduled_at` to handle nil correctly; no explicit transaction needed (single-statement auto-commit)
- [X] T007 [US1] Implement the fixed-window batch loop as the main body of the producer goroutine in `04_coordinated_table/producer.go`: compute `targetBatchSize = max(1, int(math.Round(float64(rate) * window.Seconds())))` with low-rate edge case — when `float64(rate)*window.Seconds() < 1` use `effectiveWindow = time.Duration(float64(time.Second)/float64(rate))` and `targetBatchSize = 1`; loop: record `windowStart := time.Now()`, call `buildBatch`, call `upsertBatch`, on success increment an atomic `insertedTotal` by `len(batch)`, observe `ps.batchSize` and `ps.batchDuration`, increment `ps.inserted` and `ps.batches`; compute `elapsed := time.Since(windowStart)`, sleep `max(0, effectiveWindow - elapsed)` via `select { case <-ctx.Done(): return; case <-time.After(sleep): }`; reference: research.md R1 pseudocode and R4 low-rate edge case

**Checkpoint**: At this point US1 is fully functional — rate accuracy ±5% verifiable with `SELECT COUNT(*)`.

---

## Phase 4: User Story 2 — Graceful Backpressure Handling (Priority: P2)

**Goal**: When storage is slow (batch duration ≥ window), the loop immediately starts the next window without sleeping and without accumulating a catch-up deficit; `producer_backpressure_total` records each such event.

**Independent Test**: Pause YDB (`docker pause`) for 10 s while producer is running at `--rate 500`. After resume, `producer_observed_rate` must return to ~500/s within one report interval with no burst, and `producer_backpressure_total` must have incremented during the pause.

- [X] T008 [US2] Add backpressure handling to the batch loop in `04_coordinated_table/producer.go`: after computing `elapsed`, when `elapsed >= effectiveWindow` increment `ps.backpressure` by 1; the existing `max(0, effectiveWindow - elapsed)` sleep expression already produces 0 sleep in this case — confirm the sleep branch is skipped correctly (no `time.After(0)` drain issue); also ensure `ps.batchErrors.Inc()` is called on `upsertBatch` error and the loop `continue`s without modifying `insertedTotal` (no deficit accumulation, no catch-up)

**Checkpoint**: At this point US2 is complete — `producer_backpressure_total` increments during slowdown, rate does not burst on recovery.

---

## Phase 5: User Story 3 — Accurate Rate Reporting (Priority: P3)

**Goal**: The producer emits a structured slog line every `reportInterval` showing `inserted_delta`, `rate_observed`, and updates the `producer_observed_rate` Prometheus gauge; the shutdown log includes final totals.

**Independent Test**: Run producer and tail stdout. A throughput log line must appear every `--report-interval` seconds. The `rate_observed` field must equal `inserted_delta / interval_s` and stay within ±5% of `--rate` during steady state.

- [X] T009 [US3] Add a report goroutine inside the producer function in `04_coordinated_table/producer.go`: launch `go func()` before the batch loop, use `time.NewTicker(reportInterval)` and a `select` on `ticker.C` / `ctx.Done()`; on each tick snapshot `insertedTotal` (atomic load), compute `delta = snapshot - lastSnapshot`, `rateObserved = float64(delta) / reportInterval.Seconds()`, call `slog.Info("producer stats", "inserted_total", snapshot, "inserted_delta", delta, "rate_observed", rateObserved, "interval_s", reportInterval.Seconds(), "batch_window_ms", window.Milliseconds())`, and call `ps.observedRate.Set(rateObserved)`; use `ps.targetRate.Set(float64(rate))`, `ps.windowSeconds.Set(window.Seconds())`, `ps.targetBatchSize.Set(float64(targetBatchSize))` once at startup before the loop
- [X] T010 [US3] Extend the shutdown log in `04_coordinated_table/producer.go`: after the batch loop exits, load final `insertedTotal`, compute `finalRate = float64(insertedTotal) / time.Since(ps.startTime).Seconds()`, and call `slog.Info("producer stopping", "total_inserted", insertedTotal, "rate_observed", finalRate)`; set `ps.up.Set(0)` on exit (may already be in main.go defer — confirm no double-set)

**Checkpoint**: All three user stories are now independently functional and observable.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T011 [P] Run `go vet ./...` from repository root and fix any type errors, unused imports, or mismatched function signatures introduced across `04_coordinated_table/`
- [X] T012 Run manual quickstart.md validation: `go run ./04_coordinated_table/ --mode producer --rate 100 --batch-window 100ms --report-interval 5s` for 60 s; confirm `rate_observed` stays within ±5% of 100 in slog output and `curl localhost:9090/metrics | grep '^producer_'` shows all expected metric names

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — start immediately
- **Phase 2 (Foundational)**: T002 and T003 can run in parallel; T004 depends on T001 + T002 + T003
- **Phase 3 (US1)**: Depends on Phase 2 completion; T005 → T006 → T007 (sequential, all in producer.go)
- **Phase 4 (US2)**: Depends on T007 (batch loop must exist before backpressure can be added)
- **Phase 5 (US3)**: Depends on T007 (batch loop) and T009 depends on T002 (ProducerStats handles)
- **Phase 6 (Polish)**: Depends on all preceding phases

### User Story Dependencies

- **US1 (P1)**: Can start after Phase 2 — no dependency on US2 or US3
- **US2 (P2)**: Depends on US1 batch loop (T007) — cannot be implemented until the loop exists
- **US3 (P3)**: Depends on US1 batch loop (T007) and ProducerStats (T002) — can be done in parallel with US2 since T008 and T009 modify different parts of the loop

### Parallel Opportunities Within Phase 2

```bash
# T002 and T003 touch different files — run in parallel:
Task T002: Create 04_coordinated_table/producer_stats.go
Task T003: Edit 04_coordinated_table/metrics.go
# Then sequentially:
Task T004: Wire mode-aware metrics in 04_coordinated_table/main.go
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Add CLI flags (T001)
2. Complete Phase 2: ProducerStats + metrics wiring (T002–T004)
3. Complete Phase 3: Core batch algorithm (T005–T007)
4. **STOP and VALIDATE**: `SELECT COUNT(*) FROM coordinated_tasks` after 60 s run at `--rate 100`
5. If ±5% passes, MVP is shippable

### Incremental Delivery

1. Setup + Foundational → Foundation ready (T001–T004)
2. Add US1 → Rate accuracy verified → MVP
3. Add US2 → Backpressure verified → Robust MVP
4. Add US3 → Observability complete → Production ready
5. Polish → `go vet` clean, quickstart confirmed

---

## Notes

- All changes are confined to `04_coordinated_table/` — no other packages touched
- `producer.go` is a full rewrite; read the existing file before starting T005 to understand the current function signatures and what to preserve (e.g., existing `Produce` function signature and how it's called from `main.go`)
- `[P]` tasks touch different files and have no shared state — safe to implement concurrently
- No schema migrations needed; `coordinated_tasks` table is unchanged
- Low-rate edge case (rate × window < 1) must be handled before US1 checkpoint — it is part of T007
