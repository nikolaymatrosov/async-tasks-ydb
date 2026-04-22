# Data Model: Prometheus Metrics Integration

**Branch**: `006-prometheus-metrics` | **Date**: 2026-04-22

## Entities

### Stats

The `Stats` struct is the central holder of per-worker metrics state. It is created once per worker process in `main()` and passed by pointer to both the metrics HTTP handler and the worker goroutine.

**Before (current)**:
```
Stats {
    workerID        string
    partitionsOwned atomic.Int64
    tasksProcessed  atomic.Int64
    tasksLocked     atomic.Int64
    tasksErrors     atomic.Int64
    startTime       time.Time
}
```

**After (target)**:
```
Stats {
    workerID   string
    startTime  time.Time
    registry   *prometheus.Registry      // per-worker registry; owns all metrics below
    processed  prometheus.Counter        // coordinator_tasks_processed_total
    locked     prometheus.Counter        // coordinator_tasks_locked_total
    errors     prometheus.Counter        // coordinator_tasks_errors_total
    partitions prometheus.Gauge          // coordinator_partitions_owned
    up         prometheus.Gauge          // coordinator_up (always 1)
}
```

**Construction** (`newStats(workerID string) *Stats`):
1. Create `registry = prometheus.NewRegistry()`
2. Register `collectors.NewGoCollector()` and `collectors.NewProcessCollector(...)` on `registry`
3. Create each metric with `ConstLabels: prometheus.Labels{"worker_id": workerID}`
4. Register all five metrics on `registry`
5. Set `up.Set(1)`
6. Return `&Stats{...}`

**Relationships**:
- `Stats` owns its `registry`; the registry lifetime equals the process lifetime
- `metricsHandler` receives `stats.registry` and calls `promhttp.HandlerFor(registry, ...)`
- Worker goroutine holds `*Stats` and calls `.Add()` / `.Set()` on the metric fields

---

### Metric Descriptors

Each metric is created with `prometheus.CounterOpts` or `prometheus.GaugeOpts` and registered once. The `worker_id` label value is fixed at creation time via `ConstLabels`.

| Field | Prometheus Name | Type | Help |
|-------|----------------|------|------|
| `processed` | `coordinator_tasks_processed_total` | Counter | Cumulative tasks marked completed by this worker |
| `locked` | `coordinator_tasks_locked_total` | Counter | Cumulative tasks locked (includes retries) |
| `errors` | `coordinator_tasks_errors_total` | Counter | Cumulative failed lock or complete operations |
| `partitions` | `coordinator_partitions_owned` | Gauge | Current number of partitions owned by this worker |
| `up` | `coordinator_up` | Gauge | 1 if the worker process is running, 0 otherwise |

All five metrics carry `ConstLabels: prometheus.Labels{"worker_id": workerID}`.

---

### Worker Registry

The `prometheus.Registry` instance held by `Stats` contains:
- The five application metrics above
- `collectors.NewGoCollector()` — standard Go runtime metrics (`go_goroutines`, `go_memstats_*`, etc.)
- `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})` — process metrics (`process_open_fds`, `process_cpu_seconds_total`, etc.)

---

## Read-Back Pattern for display.go

`display.go`'s `print()` function reads current metric values for console display. Since `prometheus.Counter` and `prometheus.Gauge` have no public `Value()` method, values are read via the `dto.Metric` write-back:

```go
func readCounter(c prometheus.Counter) int64 {
    var m dto.Metric
    _ = c.Write(&m)
    return int64(m.GetCounter().GetValue())
}

func readGauge(g prometheus.Gauge) int64 {
    var m dto.Metric
    _ = g.Write(&m)
    return int64(m.GetGauge().GetValue())
}
```

These helpers replace the `s.tasksProcessed.Load()` etc. calls in `print()`.

---

## File Change Map

| File | Change |
|------|--------|
| `04_coordinated_table/display.go` | Replace `atomic.Int64` fields with Prometheus metric fields; add `registry` field; update `newStats()`; replace `.Load()` calls with `readCounter`/`readGauge` helpers |
| `04_coordinated_table/metrics.go` | Replace hand-written `http.HandlerFunc` body with `promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{})` |
| `04_coordinated_table/worker.go` | Replace `s.tasksProcessed.Add(1)` etc. with `s.processed.Add(1)` etc. (4 call sites) |
| `04_coordinated_table/main.go` | Update `metricsHandler(stats, workerID)` call — workerID arg is no longer needed since it's baked into Stats |
| `go.mod` | `go mod tidy` promotes `github.com/prometheus/client_golang` to direct require |

No new files. No schema changes. No changes outside `04_coordinated_table/`.
