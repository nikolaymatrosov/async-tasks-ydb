# Tasks: Prometheus client_golang Metrics

**Input**: Design documents from `specs/006-prometheus-metrics/`  
**Branch**: `006-prometheus-metrics`

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: User story this task belongs to (US1 = P1, US2 = P2)

---

## Phase 1: Setup

**Purpose**: Promote `prometheus/client_golang` from indirect to direct dependency so imports compile cleanly.

- [X] T001 Run `go get github.com/prometheus/client_golang@v1.23.2 && go mod tidy` from repo root; confirm `go.mod` lists the package as a direct `require` entry (no `// indirect` suffix)

---

## Phase 2: Foundational (Blocking Prerequisite)

**Purpose**: Rewrite the `Stats` struct and its constructor so all five `coordinator_*` metrics are backed by Prometheus objects. Both user stories depend on this phase.

**⚠️ CRITICAL**: US1 and US2 work cannot begin until this phase is complete — `metrics.go` and `worker.go` both reference `Stats` fields by name.

- [X] T002 Rewrite `Stats` struct in `04_coordinated_table/display.go`: replace the four `atomic.Int64` fields (`partitionsOwned`, `tasksProcessed`, `tasksLocked`, `tasksErrors`) with `prometheus.Counter` fields (`processed`, `locked`, `errors`) and a `prometheus.Gauge` field (`partitions`); add `up prometheus.Gauge` and `registry *prometheus.Registry` fields; keep `workerID string` and `startTime time.Time`
- [X] T003 Rewrite `newStats(workerID string) *Stats` in `04_coordinated_table/display.go`: create `prometheus.NewRegistry()`, create each metric using `prometheus.NewCounter`/`prometheus.NewGauge` with `ConstLabels: prometheus.Labels{"worker_id": workerID}` and the existing HELP strings, register all five metrics on the registry, call `up.Set(1)`, return the populated struct
- [X] T004 Add `readCounter(c prometheus.Counter) int64` and `readGauge(g prometheus.Gauge) int64` helpers in `04_coordinated_table/display.go` using the `dto.Metric` write-back pattern (`c.Write(&m); return int64(m.GetCounter().GetValue())`); update `print()` to call these helpers instead of `.Load()` on the now-removed atomics

**Checkpoint**: `go vet ./04_coordinated_table/` must pass (display.go compiles) before proceeding.

---

## Phase 3: User Story 1 — Scrape Metrics (Priority: P1) 🎯 MVP

**Goal**: Replace the hand-written Prometheus text formatter with `promhttp.HandlerFor`; update all worker call sites; keep the `/metrics` endpoint behaviour identical for scrapers.

**Independent Test**: `curl -s localhost:9090/metrics | grep -E "^coordinator_"` returns all five `coordinator_*` metrics with correct types and non-negative values after the worker processes at least one task. `curl -s localhost:9090/other` returns HTTP 404.

- [X] T005 [P] [US1] Rewrite `metricsHandler` in `04_coordinated_table/metrics.go`: change signature to `metricsHandler(s *Stats) http.Handler`; create `inner := promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{})` inside the function; return an `http.HandlerFunc` that 404s on non-`/metrics` paths and delegates to `inner` on `/metrics`; remove all `fmt.Fprintf` Prometheus text lines and the `"fmt"` import
- [X] T006 [P] [US1] Update the four worker call sites in `04_coordinated_table/worker.go` to use the renamed Stats fields: `s.partitionsOwned.Add(±1)` → `s.partitions.Add(±1)` (lines ~91, ~97); `s.tasksErrors.Add(1)` → `s.errors.Add(1)` (lines ~139, ~297); `s.tasksLocked.Add(1)` → `s.locked.Add(1)` (line ~155); `s.tasksProcessed.Add(1)` → `s.processed.Add(1)` (line ~306)
- [X] T007 [US1] Update `metricsHandler` call in `04_coordinated_table/main.go` line ~96: remove the `workerID` second argument so it reads `go http.ListenAndServe(addr, metricsHandler(stats)) //nolint:errcheck`
- [ ] T008 [US1] Validate US1: (manual — requires live YDB instance) run `go vet ./04_coordinated_table/`; start the worker (`go run ./04_coordinated_table/ --mode worker ...`); scrape `curl -s localhost:9090/metrics`; confirm all five `coordinator_*` metrics appear with their HELP/TYPE lines and a `worker_id` label; confirm `curl -s -o /dev/null -w "%{http_code}" localhost:9090/other` returns `404`; confirm display.go stats log output matches scrape values

**Checkpoint**: US1 complete — existing Unified Agent and Prometheus scrape configs require zero changes.

---

## Phase 4: User Story 2 — Default Collectors (Priority: P2)

**Goal**: Ensure Go runtime and process metrics appear automatically in the `/metrics` response.

**Independent Test**: `curl -s localhost:9090/metrics | grep go_goroutines` returns a non-empty line. `curl -s localhost:9090/metrics | grep process_open_fds` returns a non-empty line.

- [X] T009 [US2] Add `collectors.NewGoCollector()` and `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})` registrations to `newStats()` in `04_coordinated_table/display.go`, immediately after `registry` is created and before the application metrics are registered; add the `"github.com/prometheus/client_golang/prometheus/collectors"` import
- [ ] T010 [US2] Validate US2: (manual — requires live YDB instance) restart the worker; scrape `/metrics`; confirm `go_goroutines`, `go_memstats_alloc_bytes`, `process_open_fds`, and `process_cpu_seconds_total` are all present in the response body

**Checkpoint**: US2 complete — Go runtime and process metrics available with no additional instrumentation code.

---

## Phase 5: Polish & Build Gate

**Purpose**: Final verification and go.mod hygiene.

- [X] T011 [P] Run `go vet ./04_coordinated_table/` and confirm zero diagnostics; confirm `go build -o /dev/null ./04_coordinated_table/` succeeds (constitution build gate)
- [X] T012 [P] Run `grep prometheus go.mod` and confirm `github.com/prometheus/client_golang` appears without `// indirect`; run `go mod tidy` one final time to ensure no stale indirect entries were introduced

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 — **blocks US1 and US2**
- **US1 (Phase 3)**: Depends on Phase 2 completion; T005 and T006 can run in parallel; T007 depends on T005
- **US2 (Phase 4)**: Depends on Phase 2 completion; can run concurrently with US1 if separate developer
- **Polish (Phase 5)**: Depends on US1 and US2 completion

### Within-Story Dependencies

```
T001 (Setup)
  └─ T002 → T003 → T004 (Foundational, sequential — same file)
                ├─ T005 [P] (metrics.go)
                ├─ T006 [P] (worker.go)
                │     T007 (main.go, after T005)
                │     T008 (validation)
                └─ T009 (display.go, US2)
                      T010 (validation)
T011, T012 [P] (after US1 + US2)
```

### Parallel Opportunities

```bash
# After Phase 2 completes, US1 implementation tasks T005 and T006 can run in parallel:
Task: "Rewrite metricsHandler in metrics.go"           # T005
Task: "Update worker.go call sites"                    # T006

# T011 and T012 run in parallel (different commands, no shared state):
Task: "go vet + build gate"                            # T011
Task: "go mod tidy + grep verify"                      # T012
```

---

## Implementation Strategy

### MVP (User Story 1 only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL — blocks all stories)
3. Complete Phase 3: US1 (T005 → T006 in parallel → T007 → T008)
4. **STOP and VALIDATE**: scrape `/metrics`, confirm all 5 coordinator metrics, confirm 404 for other paths
5. Existing Unified Agent pipeline continues to work unchanged

### Full Delivery

1. MVP above
2. Phase 4: US2 — add default collectors, validate runtime metrics
3. Phase 5: Polish — build gate + go.mod verification

---

## Notes

- [P] tasks touch different files and have no intra-phase dependencies
- T002–T004 are sequential (all modify `display.go`)
- T007 must follow T005 because the handler signature changes
- No automated tests — validation is manual `curl` + `go vet` per constitution
- US3 (extend to 01/02/03 examples) is explicitly out of scope for this feature (see research.md decision 8)
