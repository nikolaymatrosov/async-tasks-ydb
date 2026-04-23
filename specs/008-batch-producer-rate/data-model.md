# Data Model: Batch Producer Rate Control

**Feature**: 008-batch-producer-rate
**Date**: 2026-04-22

## Scope

This feature introduces no new persistent entities. The `coordinated_tasks` table (owned by feature 004) is unchanged; only the producer's write pattern changes. The entities below are **runtime-only** structures living inside the producer process.

## Persistent Entities (unchanged)

### Task (table: `coordinated_tasks`)

See [data-model.md for feature 004](../004-coordinated-table-workers/data-model.md). Every field behaves as before; this feature only changes how rows are inserted (multi-row `UPSERT` instead of single-row), not the schema or the per-row semantics.

**Invariants preserved by this feature**:

- `id` continues to be a fresh UUID per task.
- `hash = murmur3.Sum32([]byte(id))` cast to `Int64`.
- `partition_id = uint16((hash & 0x7FFFFFFFFFFFFFFF) % partitions)`.
- `priority` is a uniform random `Uint8`.
- `status` is always `"pending"` on insert.
- `created_at = time.Now().UTC()` captured **per row**, not per batch — so timestamps remain unique across the batch.
- `scheduled_at` is set for ~10% of rows (`rand.Intn(10) == 0`) to a time `5–30s` in the future.

## Runtime Entities (new, in-memory only)

### Batch

A group of task rows assembled over one time window and submitted as one YDB `UPSERT`.

| Field         | Type                  | Description                                                     |
|---------------|-----------------------|-----------------------------------------------------------------|
| rows          | `[]taskRow`           | Rows prepared for submission; length ≤ `targetBatchSize`        |
| windowStart   | `time.Time`           | Wall-clock moment the batch began assembling                    |
| windowEnd     | `time.Time`           | `windowStart.Add(window)` — hard deadline for sleep-to-next     |

**Lifecycle**:

1. **Assemble** — allocate `rows` with `make([]taskRow, 0, targetBatchSize)`, append `targetBatchSize` freshly-generated rows.
2. **Submit** — pass `rows` as `$records` (a YDB `List<Struct>`) to a single `UPSERT ... FROM AS_TABLE($records)` via `db.Query().Exec(...)`.
3. **Record** — on success, increment `producer_inserted_total` by `len(rows)` and `producer_batches_total` by 1; observe `producer_batch_size` and `producer_batch_duration_seconds`.
4. **Discard** — `rows` is released to GC; the struct is reused for the next window (or re-allocated, trivially cheap).

**Invariants**:

- `len(rows) == targetBatchSize` under normal operation. Smaller only if `ctx.Done()` fires during assembly (shutdown).
- A Batch is never submitted while another Batch is in flight (serialisation guarantee per FR-004).

### taskRow

A single in-memory row, used only during batch assembly.

| Field         | Type                  | YDB type                      |
|---------------|-----------------------|-------------------------------|
| id            | `string`              | `Utf8`                        |
| hash          | `int64`               | `Int64`                       |
| partitionID   | `uint16`              | `Uint16`                      |
| priority      | `uint8`               | `Uint8`                       |
| payload       | `string`              | `Utf8`                        |
| createdAt     | `time.Time`           | `Timestamp`                   |
| scheduledAt   | `*time.Time` (or `sql.Null[time.Time]`) | `Optional<Timestamp>`|

`scheduledAt` is `nil` for ~90% of rows; for the other 10% it carries a future timestamp. At submission time it is wrapped via `types.NullableTimestampValue`.

### TimeWindow (configuration)

The duration over which one Batch is assembled and submitted.

| Field          | Type            | Source                                  | Default   |
|----------------|-----------------|-----------------------------------------|-----------|
| duration       | `time.Duration` | `--batch-window` CLI flag               | `100ms`   |
| targetRate     | `int`           | `--rate` CLI flag                       | `100`     |
| targetBatchSize| `int`           | Derived: `max(1, round(rate*duration))` | n/a       |
| reportInterval | `time.Duration` | `--report-interval` CLI flag            | `5s`      |

**Low-rate adjustment**: if `rate * duration.Seconds() < 1`, the effective window is stretched to `1s / rate` and `targetBatchSize = 1`. This keeps the 1-task/sec edge case (AS3) correct.

### ProducerStats

Runtime singleton (per producer process) holding the Prometheus registry and metric handles. Defined in new file `producer_stats.go`.

| Field               | Type                  | Purpose                                                     |
|---------------------|-----------------------|-------------------------------------------------------------|
| registry            | `*prometheus.Registry`| Registry passed to `metricsHandler`                         |
| startTime           | `time.Time`           | Used to compute uptime in shutdown log                      |
| up                  | `prometheus.Gauge`    | `1` running / `0` stopped                                   |
| targetRate          | `prometheus.Gauge`    | Set once at startup from `--rate`                           |
| windowSeconds       | `prometheus.Gauge`    | Set once at startup from `--batch-window`                   |
| targetBatchSize     | `prometheus.Gauge`    | Set once at startup                                         |
| inserted            | `prometheus.Counter`  | Incremented by `len(batch)` per successful batch            |
| batches             | `prometheus.Counter`  | Incremented by 1 per successful batch                       |
| batchErrors         | `prometheus.Counter`  | Incremented by 1 per UPSERT error                           |
| batchSize           | `prometheus.Histogram`| Observe `len(batch)` per batch                              |
| batchDurationSeconds| `prometheus.Histogram`| Observe UPSERT latency per batch                            |
| observedRate        | `prometheus.Gauge`    | Set every `reportInterval` from `(delta / interval)`        |
| backpressure        | `prometheus.Counter`  | Incremented when a batch's duration ≥ `window.duration`     |

All metrics are **unlabeled** — the producer is a single-process-per-mode demo, so cardinality is 1.

## State Transitions

### Producer Mode Lifecycle

```text
   start
     │
     ▼
  ┌─────────┐       ctx.Done
  │ running ├───────────────────▶ shutdown ─▶ exit
  └────┬────┘                       ▲
       │ every window.duration      │
       ▼                            │
  ┌──────────┐  success           │
  │ assemble ├──────▶ submit ─────┤
  └──────────┘       │            │
                     │ error      │
                     ▼            │
                 (log, skip) ─────┘
```

Every transition from `assemble` → `submit` → back to `assemble` corresponds to exactly one `Batch`, and the gap between them includes `sleep = max(0, window.duration - elapsed)` to align on window boundaries without overlap and without catch-up.

## No Schema Changes

No new migration files. The existing `migrations/20260329000005_create_coordinated_tasks.sql` (feature 004) already defines the `coordinated_tasks` table with every column this feature writes (`id`, `hash`, `partition_id`, `priority`, `status`, `payload`, `created_at`, `scheduled_at`).
