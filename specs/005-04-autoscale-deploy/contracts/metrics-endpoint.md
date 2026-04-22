# Contract: Prometheus Metrics Endpoint

**Component**: `04_coordinated_table` binary  
**Endpoint**: `GET http://localhost:{metrics-port}/metrics`  
**Default port**: `9090`  
**CLI flag**: `--metrics-port` (int, default `9090`)  
**Format**: Prometheus text exposition format (v0.0.4)

## Availability

- **Worker mode**: endpoint starts before `runWorker` is called; metrics are live from startup.
- **Producer mode**: endpoint starts before `runProducer` is called; only `coordinator_up` emits (value `1`).
- **On shutdown**: endpoint stops responding when the process exits; Unified Agent's `metrics_pull` will record a scrape error but this is expected.

## Response Format

```
# HELP coordinator_tasks_processed_total Cumulative tasks marked completed by this worker
# TYPE coordinator_tasks_processed_total counter
coordinator_tasks_processed_total{worker_id="<uuid>"} 42

# HELP coordinator_tasks_locked_total Cumulative tasks locked (includes retries)
# TYPE coordinator_tasks_locked_total counter
coordinator_tasks_locked_total{worker_id="<uuid>"} 50

# HELP coordinator_tasks_errors_total Cumulative failed lock or complete operations
# TYPE coordinator_tasks_errors_total counter
coordinator_tasks_errors_total{worker_id="<uuid>"} 2

# HELP coordinator_partitions_owned Current number of partitions owned by this worker
# TYPE coordinator_partitions_owned gauge
coordinator_partitions_owned{worker_id="<uuid>"} 12

# HELP coordinator_up 1 if the worker process is running, 0 otherwise
# TYPE coordinator_up gauge
coordinator_up{worker_id="<uuid>"} 1
```

## Constraints

- Counter values are process-lifetime totals; they reset when the process restarts.
- The `worker_id` label value is fixed for the lifetime of a process (generated once in `main()`).
- The endpoint is unauthenticated (internal VM traffic only; not exposed via NAT).
- HTTP response status is always `200 OK` for `/metrics`; any non-`/metrics` path returns `404`.
- Scrape latency target: < 5ms (all values come from atomic reads with no I/O).
