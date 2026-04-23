# Quickstart: Batch Producer Rate Control

**Feature**: 008-batch-producer-rate

This feature changes only how the `04_coordinated_table` producer issues inserts — it batches rows per time window and emits Prometheus metrics so you can see whether the producer is keeping up with the configured rate.

## Prerequisites

- Go 1.26+
- A running YDB instance (local Docker or YDB Serverless)
- Existing migrations applied (`coordinated_tasks` table present, from feature 004)
- `curl` (optional, for poking `/metrics`)

## 1. Apply migrations (idempotent; skip if already done)

```bash
goose -dir ./migrations ydb \
  "grpc://localhost:2136/local?go_query_mode=scripting&go_fake_tx=scripting&go_query_bind=declare,numeric" up
```

## 2. Run the producer with batching

```bash
YDB_ANONYMOUS_CREDENTIALS=1 go run ./04_coordinated_table/ \
  --endpoint grpc://localhost:2136 \
  --database /local \
  --mode producer \
  --rate 500 \
  --batch-window 100ms \
  --report-interval 5s \
  --metrics-port 9090
```

### New flags introduced by this feature

| Flag                | Default | Meaning                                                           |
|---------------------|---------|-------------------------------------------------------------------|
| `--batch-window`    | `100ms` | Duration of one batching window; rows per batch = `rate * window` |
| `--report-interval` | `5s`    | How often the slog throughput line and `producer_observed_rate` gauge are refreshed |

## 3. Confirm steady-state rate

Every `--report-interval` the producer logs a throughput line:

```json
{"time":"2026-04-22T10:00:05Z","level":"INFO","msg":"producer stats",
 "inserted_total":2500,"inserted_delta":2500,"rate_observed":500,
 "interval_s":5,"batch_window_ms":100}
```

**Acceptance**: after a 30-second warm-up, `rate_observed` sits within ±5% of `--rate` (spec SC-001).

## 4. Visualise "are we keeping up?" via Prometheus

The metrics endpoint exposes producer-side counters on the port you gave to `--metrics-port`:

```bash
curl -s localhost:9090/metrics | grep '^producer_'
```

Example output (truncated):

```text
producer_up 1
producer_target_rate 500
producer_window_seconds 0.1
producer_target_batch_size 50
producer_inserted_total 2500
producer_batches_total 50
producer_batch_errors_total 0
producer_backpressure_total 0
producer_observed_rate 500
producer_batch_size_bucket{le="10"} 0
producer_batch_size_bucket{le="100"} 50
producer_batch_duration_seconds_bucket{le="0.05"} 50
```

### Key signals for a Grafana dashboard

| Question                                      | PromQL                                                                                      |
|-----------------------------------------------|---------------------------------------------------------------------------------------------|
| Is observed rate hitting target?              | `rate(producer_inserted_total[30s]) / producer_target_rate`                                 |
| Are we falling behind (storage slow)?         | `increase(producer_backpressure_total[1m])` > 0                                             |
| How much headroom before missing a window?    | `histogram_quantile(0.95, rate(producer_batch_duration_seconds_bucket[1m])) / producer_window_seconds` |
| Are batches full?                             | `histogram_quantile(0.5, rate(producer_batch_size_bucket[1m]))` vs `producer_target_batch_size` |

A value of `producer_backpressure_total` that keeps increasing is the single clearest signal that the system **cannot** keep up with the configured rate — either the target needs to drop or storage throughput needs to rise.

## 5. Induce backpressure to see the metrics move (optional)

Push the rate past what local YDB can absorb:

```bash
YDB_ANONYMOUS_CREDENTIALS=1 go run ./04_coordinated_table/ \
  --endpoint grpc://localhost:2136 \
  --database /local \
  --mode producer \
  --rate 10000 \
  --batch-window 100ms
```

Expected:

- `rate(producer_inserted_total[30s])` plateaus below 10000/s.
- `producer_backpressure_total` increases as batches take ≥ 100ms.
- `producer_observed_rate` < `producer_target_rate` in the slog line.
- On Ctrl-C the producer exits cleanly (signal handling unchanged); no in-flight batch is submitted twice.

## 6. Verify the acceptance tests from the spec

| Scenario                       | How to check                                                                           | Pass criteria                                    |
|--------------------------------|----------------------------------------------------------------------------------------|--------------------------------------------------|
| SC-001 rate accuracy           | Run at `--rate 100` for 60s, then `SELECT COUNT(*) FROM coordinated_tasks`             | Count in `[5700, 6300]` (±5%)                    |
| SC-001 low rate                | Run at `--rate 1` for 30s, then count                                                  | Count in `[28, 32]`                              |
| SC-002 bounded memory          | Observe `producer_batch_size` max bucket during any run                                | Never exceeds `target_batch_size`                |
| SC-004 no post-recovery burst  | Pause YDB (e.g. `docker pause`) for 10s during a run, then resume                      | `producer_observed_rate` returns to target within one report interval, no spike above target |
| SC-003 log interval            | Tail stdout                                                                            | Throughput log line every `--report-interval`, no gaps |

## 7. Troubleshooting

| Symptom                                            | Likely cause                               | Fix                                               |
|----------------------------------------------------|--------------------------------------------|---------------------------------------------------|
| `producer_batches_total` not advancing             | UPSERT error, see `producer_batch_errors_total` and stderr | Check `--endpoint` / creds; run `goose up`        |
| `producer_observed_rate` ≈ 0                       | `ctx.Done()` was triggered                 | Remove stray `kill`; verify signal handler        |
| `producer_target_batch_size` is `1` at high rate   | `--batch-window` too small (e.g. 1ms)      | Use the default `100ms` or larger                 |
| `producer_backpressure_total` rising at low rate   | YDB endpoint unreachable; requests retrying| Check network to `--endpoint`                     |
