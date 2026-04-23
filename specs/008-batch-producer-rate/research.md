# Research: Batch Producer Rate Control

**Feature**: 008-batch-producer-rate
**Date**: 2026-04-22

## R1: Rate-Control Algorithm — Fixed-Window Batching vs. Per-Item Ticker

**Decision**: Fixed-window batching. Every `window` duration (default 100ms), assemble a batch of `batchSize = max(1, round(rate * window))` rows and submit it in a single YDB `UPSERT`. Wait for the submission to finish before sleeping until the next window boundary.

**Rationale**:

- `time.NewTicker(time.Second / rate)` (current implementation) does not deliver accurate rates above a few hundred per second — Go timers on Darwin/Linux have ~1–10ms resolution, and per-item SDK round-trips dominate. At 1,000/s the ticker would need 1ms period, which the runtime cannot honour reliably.
- Batch-per-window moves the rate contract off the timer and onto row count: at 1,000/s × 100ms = 100 rows per window, ±1 timer tick (~1ms) only shifts ±10 rows — well inside the ±5% budget (FR-002, SC-001).
- Serialising windows (next window starts only after the current UPSERT returns) satisfies FR-003 (bounded memory to one window's worth) and FR-004 (no overlapping submissions) automatically, without explicit locks or queues.

**Next-window scheduling**: use `time.Sleep(until(windowStart + window))` rather than `time.Ticker`. A ticker would buffer missed ticks and produce a burst after a slow UPSERT — the spec's FR-006 / SC-004 explicitly forbid that. Sleeping to the *next* boundary (dropping intervening windows) keeps the rate at-most-target on recovery.

**Alternatives considered**:

- **Leaky-bucket / token-bucket** (`golang.org/x/time/rate`): Smoother instantaneous rate, but still submits one row per token — same SDK round-trip-per-task problem at 1,000+/s. Would also require a new direct dependency the constitution's Tech Constraints flag.
- **Goroutine pool with per-worker tickers**: Solves throughput but makes the rate a noisy sum of N independent tickers; harder to keep within ±5%.
- **Keep per-task ticker, increase concurrency**: Does not address the per-task round-trip cost and bloats connection use.

## R2: YDB Batch UPSERT via `AS_TABLE($records)`

**Decision**: Submit the batch as a single `UPSERT INTO coordinated_tasks SELECT ... FROM AS_TABLE($records)` where `$records` is a `List<Struct<...>>` built via `types.ListValue` + `types.StructValue`, bound through `ParamsBuilder().Param("$records").Any(...)`.

**Rationale**:

- The pattern is already in-repo at [03_topic/consumer.go:184-206](../../03_topic/consumer.go#L184-L206), which upserts into `stats` from `AS_TABLE($records)` — constitution-compliant reuse.
- A single `UPSERT ... SELECT FROM AS_TABLE(...)` is atomic per statement: all rows in the batch land together, matching FR-001 ("submitted together as a single unit of work").
- Avoids N-queries-per-batch, which would reintroduce per-task latency.

**Query shape**:

```sql
UPSERT INTO coordinated_tasks
SELECT
    id, hash, partition_id, priority, "pending" AS status,
    payload, created_at, scheduled_at
FROM AS_TABLE($records);
```

`$records` is `List<Struct<id:Utf8, hash:Int64, partition_id:Uint16, priority:Uint8, payload:Utf8, created_at:Timestamp, scheduled_at:Optional<Timestamp>>>`. `scheduled_at` uses `types.NullableTimestampValue` so the ~10% of rows without a schedule can pass `nil` (FR-008).

**Transaction settings**: `db.Query().Exec` without an explicit tx is sufficient — each batch is a single statement. The previous code used `DoTx(... SerializableReadWrite)` per task for strict isolation, but batch UPSERT into a single table with no read-modify-write doesn't need serialisable; the default `OnlineReadOnly`-compatible auto-commit is fine. This halves round-trips (no `BEGIN`/`COMMIT`).

**Alternatives considered**:

- **Multiple single-row UPSERTs in one `DoTx`**: Works, but bundles many parameter sets rather than one list — larger wire payload and more SDK bookkeeping. No semantic advantage over `AS_TABLE`.
- **Bulk Upsert API (`table.BulkUpsert`)**: Higher throughput, but lives on the deprecated table-service API rather than the `query` package the rest of this example uses. Mixing APIs for one demo hurts readability.

## R3: Backpressure and Burst Suppression on Storage Slowdown

**Decision**: Serialise windows and *drop* missed windows — if a batch UPSERT takes longer than `window`, the next batch starts immediately at current wall-clock, but its size is still `rate * window` (not the accumulated deficit).

**Rationale**:

- FR-006 / SC-004 require no compensating burst on recovery. Accumulating deficit and catching up ("catch-up semantics") would violate this.
- Dropping missed windows means the long-run rate during a slowdown is the storage's actual throughput, not the configured target — exactly what SC-002 calls for (memory never exceeds one batch).
- On recovery, the producer resumes at the configured rate; there is no queue to flush, so no burst (SC-004).

**Loop shape** (pseudocode):

```go
for {
    windowStart := time.Now()
    batch := buildBatch(batchSize)        // in-memory only
    if err := upsertBatch(ctx, batch); err != nil { log; continue }
    inserted += len(batch)
    elapsed := time.Since(windowStart)
    if sleep := window - elapsed; sleep > 0 {
        select { case <-ctx.Done(): return; case <-time.After(sleep): }
    }
    // if elapsed >= window: no sleep, next window starts immediately
}
```

**Alternatives considered**:

- **Catch-up (deficit-aware)**: Rejected — violates FR-006.
- **Rate limiter with burst bucket**: Token-bucket's burst parameter would permit post-slowdown bursts; same FR-006 conflict if burst > 0, and if burst = 0, it degrades to our serial loop with extra complexity.

## R4: Choice of Default Batch Window

**Decision**: 100ms, configurable via `--batch-window` flag.

**Rationale**:

- At `rate = 100/s` → 10 rows/batch (above 1, below storage row-count limits).
- At `rate = 10,000/s` → 1,000 rows/batch — within YDB's documented ~10,000 row limit per `UPSERT`.
- At `rate = 1/s` → 0.1 rows/batch, rounded up to 1 every 10 windows (i.e., one row per second still works because `batchSize = max(1, round(rate*window))` triggers every 10th window; in practice we instead run one row per `1/rate` seconds when `rate*window < 1`).
- 100ms is short enough that a single slow batch doesn't let the observed rate deviate by more than one window's worth over a 30-second measurement — safely inside ±5%.

**Low-rate edge case** (rate × window < 1): compute `effectiveWindow = max(window, 1s/rate)` and `batchSize = 1` so sub-1-row-per-window rates still tick at the correct cadence. Spec scenario AS3 (1/s → 28–32 over 30s) is satisfied.

**Alternatives considered**:

- **1-second window**: Coarser; a single slow batch gives a noticeable gap in the rate graph.
- **10ms window**: Too fine — Go timer jitter becomes a meaningful fraction of the window again.

## R5: Throughput Reporting

**Decision**: A separate `time.Ticker` (default 5s, configurable via `--report-interval`) logs `inserted` delta per interval and `rate_observed = delta / interval`. Reporting is decoupled from the batch loop — it uses its own goroutine / select branch.

**Rationale**:

- FR-005 requires periodic logging; separating it from the batch loop keeps the rate-control path free of logging-related latency.
- The log must report observed rate, not configured target (FR-005 scenario 2 / SC-003), so the counter is sampled at report time rather than computed from flags.
- Uses `slog.Info` with structured fields `{inserted_total, inserted_delta, rate_observed, interval_s, batch_window_ms}` per constitution principle V.

**Shutdown stats**: existing "producer stopping" log is extended to include `total_inserted` and final `rate_observed` (total / elapsed).

**Alternatives considered**:

- **Log per batch**: Too noisy at 10 logs/s (100ms window). Also couples logging to the hot path.
- **Prometheus-only (no slog)**: dropped — slog gives quick feedback during `go run`; metrics are for dashboards. We do both (see R6).

## R6: Prometheus Metrics for Producer Mode

**Decision**: Introduce a new `ProducerStats` struct (new file `producer_stats.go`) with its own `*prometheus.Registry`, registered by `main.go` when `--mode=producer`. Keep the worker's `Stats` (`display.go`) unchanged. Generalise `metricsHandler` to accept a `*prometheus.Registry` so either registry can be served through the same `/metrics` handler.

**Rationale**:

- The metrics server is already wired in [main.go:96](../../04_coordinated_table/main.go#L96) (`go http.ListenAndServe(addr, metricsHandler(stats))`), but today `stats := newStats(workerID)` is created unconditionally — in producer mode `/metrics` exposes worker counters that never increment. Fix that by branching on mode before the `ListenAndServe` call.
- Separate struct per mode (rather than one struct with optional worker- and producer-only fields) keeps the ownership model explicit and mirrors the multi-file-per-concern layout the example already uses.
- A dedicated registry per mode avoids registering unused worker collectors under producer mode (and vice versa) — cleaner scrape output.

**Metrics** (prefix `producer_`, no `worker_id` label — the producer is singleton per process):

| Metric                            | Type      | Purpose                                                                                 |
|-----------------------------------|-----------|-----------------------------------------------------------------------------------------|
| `producer_up`                     | gauge     | `1` while running, `0` on shutdown                                                      |
| `producer_target_rate`            | gauge     | Configured `--rate`; static after startup, handy for dashboards                         |
| `producer_window_seconds`         | gauge     | Configured `--batch-window` in seconds                                                  |
| `producer_target_batch_size`      | gauge     | `round(rate * window)` — expected rows per batch                                        |
| `producer_inserted_total`         | counter   | Cumulative rows successfully inserted                                                   |
| `producer_batches_total`          | counter   | Cumulative batches submitted                                                            |
| `producer_batch_errors_total`     | counter   | Cumulative batches that returned an error from YDB                                      |
| `producer_batch_size`             | histogram | Distribution of rows per submitted batch (buckets: 1,10,100,1k,10k)                     |
| `producer_batch_duration_seconds` | histogram | Distribution of UPSERT latency (buckets: 5ms,10ms,25ms,50ms,100ms,250ms,500ms,1s,2s,5s) |
| `producer_observed_rate`          | gauge     | Rows/sec computed over the last report interval                                         |
| `producer_backpressure_total`     | counter   | Windows where batch duration ≥ configured window (cannot keep up)                       |

**How this answers "keep up with the flow"**:

- `rate(producer_inserted_total[30s])` vs `producer_target_rate` → is observed rate hitting target?
- `producer_backpressure_total` increasing → storage is the bottleneck for this window.
- `histogram_quantile(0.95, rate(producer_batch_duration_seconds_bucket[1m]))` vs `producer_window_seconds` → headroom before we start dropping windows.
- `producer_batch_size` vs `producer_target_batch_size` → confirms the algorithm is assembling full batches (small batches may indicate ctx cancellation during assembly or a too-low rate).

**Registry collectors**: include `collectors.NewGoCollector()` and `collectors.NewProcessCollector(...)` to match worker behaviour and provide Go runtime / process stats on the same endpoint.

**Alternatives considered**:

- **Reuse `Stats` with optional producer fields**: Lower file count, but fields become mode-dependent and half of them stay zero in each mode. Obscures intent for a learning example.
- **Separate port for producer metrics**: Adds deployment surface area for no gain.
- **Counter without a corresponding gauge for observed rate**: A counter requires PromQL `rate(...)` to visualise — fine for dashboards, but the gauge gives an at-a-glance number in `/metrics` that matches the slog log line, helping local development.
