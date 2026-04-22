# Implementation Plan: Prometheus client_golang Metrics

**Branch**: `006-prometheus-metrics` | **Date**: 2026-04-22 | **Spec**: [spec.md](spec.md)  
**Input**: Feature specification from `specs/006-prometheus-metrics/spec.md`

## Summary

Replace the hand-rolled Prometheus text format handler in `04_coordinated_table/metrics.go` and the `atomic.Int64` counter fields in `display.go` with `github.com/prometheus/client_golang`, using a per-worker `prometheus.Registry`, `promhttp.HandlerFor` for HTTP serving, and `dto.Metric` read-back for the display loop. The five existing `coordinator_*` metric names, types, and `worker_id` labels are preserved exactly; Go runtime and process collectors are added automatically.

## Technical Context

**Language/Version**: Go 1.26 (as declared in `go.mod`)  
**Primary Dependencies**: `github.com/prometheus/client_golang v1.23.2` (promote from indirect to direct); `github.com/prometheus/client_model/go` (already indirect, used for `dto.Metric` read-back); no new version bumps needed  
**Storage**: N/A (no YDB schema changes)  
**Testing**: Manual — `go run ./04_coordinated_table/ --mode worker ...` against a live YDB instance; scrape `/metrics` with `curl` or Prometheus  
**Target Platform**: Linux (containerised, Yandex Cloud), macOS (local dev)  
**Project Type**: CLI binary — single `main.go` per example directory (constitution principle I)  
**Performance Goals**: Scrape latency < 5ms (existing contract SLA preserved; Go/process collectors use cheap `/proc` reads)  
**Constraints**: No sub-packages within `04_coordinated_table/`; all logic stays in existing `.go` files in that directory  
**Scale/Scope**: Single example (`04_coordinated_table`); ~4 files changed; P3 (extend to 01/02/03) is out of scope

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ✅ All changes are within `04_coordinated_table/`; no sub-packages introduced |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ No lifecycle changes; metrics server goroutine unchanged |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ N/A — no schema changes |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ No configuration changes |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ `display.go` stats block preserved; slog calls unchanged |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ `prometheus/client_golang v1.23.2` promoted to direct dep; no version bump; justified in research.md |

No violations. Complexity Tracking table not required.

## Project Structure

### Documentation (this feature)

```text
specs/006-prometheus-metrics/
├── plan.md              ← this file
├── spec.md
├── research.md
├── data-model.md
├── contracts/
│   └── metrics-endpoint.md
├── checklists/
│   └── requirements.md
└── tasks.md             ← created by /speckit-tasks
```

### Source Code (changes only)

```text
04_coordinated_table/
├── display.go      ← Stats struct: replace atomic.Int64 with prometheus.Counter/Gauge fields;
│                     add registry field; update newStats(); add readCounter/readGauge helpers;
│                     replace .Load() calls in print()
├── metrics.go      ← Replace hand-written HandlerFunc body with promhttp.HandlerFor(s.registry, ...)
├── worker.go       ← Replace s.tasksProcessed.Add(1) etc. with s.processed.Add(1) etc. (4 sites)
└── main.go         ← Remove workerID arg from metricsHandler call (no longer needed)

go.mod              ← `go mod tidy` promotes prometheus/client_golang to direct require
go.sum              ← updated by go mod tidy
```

No new files in `04_coordinated_table/`. No other directories touched.

## Implementation Notes

### Stats struct (display.go)

Replace the four `atomic.Int64` fields with Prometheus metric objects and add a `registry` field:

```go
type Stats struct {
    workerID  string
    startTime time.Time
    registry  *prometheus.Registry
    processed prometheus.Counter
    locked    prometheus.Counter
    errors    prometheus.Counter
    partitions prometheus.Gauge
    up        prometheus.Gauge
}
```

`newStats(workerID string)` creates the registry, registers Go/process collectors, creates and registers all five metrics with `ConstLabels{"worker_id": workerID}`, sets `up.Set(1)`, and returns the struct.

### metrics.go

The entire `metricsHandler` body reduces to a path guard + `promhttp.HandlerFor`:

```go
func metricsHandler(s *Stats) http.Handler {
    inner := promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{})
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/metrics" {
            http.NotFound(w, r)
            return
        }
        inner.ServeHTTP(w, r)
    })
}
```

### display.go read-back helpers

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

`print()` calls `readCounter(s.processed)` etc. instead of `s.tasksProcessed.Load()`.

### worker.go call sites (4 locations)

| Line (approx) | Before | After |
| --- | --- | --- |
| ~91 | `s.partitionsOwned.Add(1)` | `s.partitions.Add(1)` |
| ~97 | `s.partitionsOwned.Add(-1)` | `s.partitions.Add(-1)` |
| ~139, ~297 | `s.tasksErrors.Add(1)` | `s.errors.Add(1)` |
| ~155 | `s.tasksLocked.Add(1)` | `s.locked.Add(1)` |
| ~306 | `s.tasksProcessed.Add(1)` | `s.processed.Add(1)` |

### main.go

Update the `metricsHandler` call: remove the `workerID` second argument (now baked into Stats):

```go
go http.ListenAndServe(addr, metricsHandler(stats)) //nolint:errcheck
```

### go.mod

```sh
go get github.com/prometheus/client_golang@v1.23.2
go mod tidy
```

This moves the entry from `// indirect` to a direct `require`.

## Validation

Manual end-to-end check (constitution dev workflow):

1. `go vet ./04_coordinated_table/` — must pass with zero diagnostics
2. Start worker: `go run ./04_coordinated_table/ --mode worker --endpoint $YDB_ENDPOINT --database $YDB_DATABASE`
3. `curl -s localhost:9090/metrics | grep coordinator_` — all five metrics present with correct types
4. `curl -s localhost:9090/metrics | grep go_goroutines` — standard runtime metrics present
5. `curl -s localhost:9090/anything` — returns `404 page not found`
6. Process N tasks; re-scrape; confirm `coordinator_tasks_processed_total` equals N
7. Observe `display.go` log output matches scrape values
8. `go mod tidy && grep prometheus go.mod` — entry has no `// indirect` suffix
