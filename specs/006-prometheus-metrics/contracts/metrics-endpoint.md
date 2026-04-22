# Contract: Prometheus Metrics Endpoint (v2)

**Component**: `04_coordinated_table` binary  
**Endpoint**: `GET http://localhost:{metrics-port}/metrics`  
**Default port**: `9090`  
**CLI flag**: `--metrics-port` (int, default `9090`)  
**Format**: Prometheus text exposition format (v0.0.4) — or OpenMetrics format if requested via `Accept` header (handled by `promhttp`)

**Supersedes**: `specs/005-04-autoscale-deploy/contracts/metrics-endpoint.md`

## Availability

Same as v1:
- **Worker mode**: endpoint starts before `runWorker` is called; all metrics are live from startup.
- **Producer mode**: endpoint starts before `runProducer`; `coordinator_up` = 1, task counters remain 0.
- **On shutdown**: endpoint stops when the process exits; scrapers will record a scrape error, which is expected.

## Application Metrics (unchanged from v1)

All five metrics are preserved with identical names, types, labels, and HELP strings:

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

## Additional Metrics (new in v2)

The endpoint now also exposes standard Go runtime and process metrics registered by `collectors.NewGoCollector()` and `collectors.NewProcessCollector()`. Examples (non-exhaustive):

```
go_goroutines <N>
go_memstats_alloc_bytes <N>
go_memstats_heap_inuse_bytes <N>
process_open_fds <N>
process_resident_memory_bytes <N>
process_cpu_seconds_total <N>
```

Scrapers MUST NOT rely on the exact set of Go/process metrics, as they may vary across Go versions. All `coordinator_*` metrics are stable.

## Constraints

- Counter values are process-lifetime totals; they reset on process restart.
- The `worker_id` label value is fixed for the lifetime of a process (generated once in `main()`).
- The endpoint is unauthenticated (internal VM traffic only; not exposed via NAT).
- HTTP response status: `200 OK` for `/metrics`; `404` for any other path.
- Scrape latency target: < 5ms. Application metrics are pure in-memory reads; Go/process collectors involve cheap OS calls (`/proc` reads on Linux) that fit comfortably within this budget.
- The registry is per-worker-process (not the global `DefaultRegisterer`). No cross-process metric sharing.

## Breaking Changes from v1

None. All five `coordinator_*` metric names, types, label keys, and HELP strings are preserved exactly. The only observable difference is additional metrics in the response body.

## Scraper Compatibility

Existing Unified Agent `metrics_pull` configuration targeting this endpoint requires no changes. The additional Go/process metrics will be collected automatically.
