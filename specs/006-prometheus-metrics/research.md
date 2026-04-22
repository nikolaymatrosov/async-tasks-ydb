# Research: Prometheus client_golang Integration

**Branch**: `006-prometheus-metrics` | **Date**: 2026-04-22

## Decision 1: Per-worker registry vs. DefaultRegisterer

**Decision**: Use `prometheus.NewRegistry()` per `Stats` instance instead of `prometheus.DefaultRegisterer`.

**Rationale**: `DefaultRegisterer` is a process-global singleton. Registering metrics with `worker_id` labels against it works for a single worker but silently conflicts if two workers share a process (duplicate registration panic). A per-instance registry makes the `Stats` struct self-contained and avoids global state, consistent with constitution principle I (self-contained examples).

**Alternatives considered**:
- `DefaultRegisterer`: simpler but panics on double-register; also couples test helpers to global state.
- `MustRegister` with `prometheus.Labels`: still global; only moves the problem.

---

## Decision 2: Reading counter values in display.go

**Decision**: Read Prometheus counter/gauge values via `dto.Metric` + `Write(*dto.Metric)` for display.go, rather than keeping parallel `atomic.Int64` shadow fields.

**Rationale**: `prometheus.Counter` and `prometheus.Gauge` do not expose a public `Value() float64` method in the stable API. The canonical read-back path is:
```go
var m dto.Metric
metric.Write(&m)  // metric is prometheus.Counter or prometheus.Gauge
val := int64(m.GetCounter().GetValue())
```
`github.com/prometheus/client_model/go` (`dto` package) is already a transitive dependency. This approach keeps a single source of truth (no shadow atomics) and exercises the same data path that Prometheus scraping uses.

**Alternatives considered**:
- Keep `atomic.Int64` + increment Prometheus metric in parallel: works but two sources of truth; easy to diverge.
- Use `testutil.ToFloat64(metric)`: designed for tests, not production code.
- Use an unexported trick via `reflect`: brittle across library versions.

---

## Decision 3: Default collector registration on per-worker registry

**Decision**: Register `collectors.NewGoCollector()` and `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})` explicitly on the per-worker registry.

**Rationale**: `prometheus.NewRegistry()` creates an empty registry — it does NOT auto-register Go or process collectors (unlike `DefaultRegisterer`). To get standard runtime metrics (goroutine count, heap, process FDs, CPU), they must be registered explicitly. This satisfies FR-006 and SC-002.

**Alternatives considered**:
- Wrap `prometheus.WrapRegistererWith(labels, DefaultRegisterer)`: brings in global state and conflicts with any other code using DefaultRegisterer.
- Skip default collectors: violates FR-006.

---

## Decision 4: HTTP handler

**Decision**: Replace the hand-written `http.HandlerFunc` in `metrics.go` with `promhttp.HandlerFor(registry, promhttp.HandlerOpts{})`.

**Rationale**: `promhttp.HandlerFor` handles content negotiation (text vs. OpenMetrics), compression, and error reporting correctly. The current hand-written handler hardcodes `text/plain; version=0.0.4` and uses `fmt.Fprintf` string interpolation — it cannot negotiate OpenMetrics format and has no error handling.

**Alternatives considered**:
- `promhttp.Handler()`: uses DefaultRegisterer; incompatible with per-worker registry.

---

## Decision 5: Stats struct shape

**Decision**: Replace the four `atomic.Int64` fields in `Stats` with typed Prometheus metric vars (`prometheus.Counter` for tasks, `prometheus.Gauge` for partitions). The `coordinator_up` gauge is registered once at `newStats()` and immediately set to 1; it requires no update thereafter.

**Rationale**: Single source of truth for each value. Worker code calls `.Add(1)` / `.Set(float64)` directly on the Prometheus metric, same as the current `.Add(1)` on atomic — minimal diff.

**Method mapping**:
| Current | New |
|---------|-----|
| `s.tasksProcessed.Add(1)` | `s.processed.Add(1)` |
| `s.tasksLocked.Add(1)` | `s.locked.Add(1)` |
| `s.tasksErrors.Add(1)` | `s.errors.Add(1)` |
| `s.partitionsOwned.Add(±1)` | `s.partitions.Add(±1)` |

---

## Decision 6: Metric label implementation

**Decision**: Use plain `prometheus.Counter` / `prometheus.Gauge` (not `CounterVec` / `GaugeVec`) with `worker_id` baked in via `prometheus.Labels` at registration time.

**Rationale**: Each `Stats` instance is scoped to a single worker. There is no need for a vector that holds multiple label values — the `worker_id` is fixed for the lifetime of the Stats instance. Using `prometheus.NewCounterWith(prometheus.CounterOpts{ConstLabels: prometheus.Labels{"worker_id": workerID}, ...})` produces the same wire format (`metric_name{worker_id="..."}`) as the current hand-rolled output, with zero overhead.

**Alternatives considered**:
- `CounterVec` + `.With(labels)`: designed for dynamic label sets; adds indirection for no benefit here.

---

## Decision 7: go.mod promotion

**Decision**: Add `github.com/prometheus/client_golang v1.23.2` as a direct `require` entry by importing it in `04_coordinated_table/metrics.go`. Run `go mod tidy` to promote the indirect entry.

**Rationale**: Constitution tech-constraints section requires noting new direct dependencies in the feature plan. `v1.23.2` is already in `go.sum` as an indirect dep; no version change is needed.

---

## Decision 8: Scope of P3 (01/02/03 examples)

**Decision**: P3 (extending Prometheus metrics to other examples) is out of scope for this feature. The spec marks it as optional ("MAY"); given the constitution's preference for minimal scope, it will be tracked as a separate future feature if needed.

**Rationale**: Examples 01/02/03 have no existing `/metrics` endpoint to break, and adding one to each is independent work. Bundling it here risks scope creep and adds risk to the primary migration.
