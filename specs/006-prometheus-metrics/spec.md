# Feature Specification: Prometheus Client Library for Metrics

**Feature Branch**: `006-prometheus-metrics`  
**Created**: 2026-04-22  
**Status**: Draft  
**Input**: Replace hand-rolled Prometheus text format with `github.com/prometheus/client_golang`

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Scrape Metrics with Prometheus or Unified Agent (Priority: P1)

An operator deploys the worker and scrapes the `/metrics` endpoint with any standard Prometheus-compatible scraper (Prometheus server, Unified Agent `metrics_pull`, cURL). The metrics returned are identical in name, type, and label structure to those produced today, so no changes are required to any existing scraping or alerting configuration.

**Why this priority**: Drop-in compatibility is the primary success criterion. If existing scrapers break, the migration has no value.

**Independent Test**: Start the worker, scrape `/metrics`, and verify that all five existing metric names and types are present with correct values after processing a known number of tasks.

**Acceptance Scenarios**:

1. **Given** the worker has processed N tasks, **When** `/metrics` is scraped, **Then** `coordinator_tasks_processed_total{worker_id="..."}` equals N and has type `counter`.
2. **Given** the worker owns K partitions, **When** `/metrics` is scraped, **Then** `coordinator_partitions_owned{worker_id="..."}` equals K and has type `gauge`.
3. **Given** M errors have occurred, **When** `/metrics` is scraped, **Then** `coordinator_tasks_errors_total{worker_id="..."}` equals M.
4. **Given** the worker is running, **When** `/metrics` is scraped, **Then** `coordinator_up{worker_id="..."}` equals 1.
5. **Given** an unknown path is requested, **When** a scraper hits `/anything-else`, **Then** the server responds with 404.

---

### User Story 2 - Standard Prometheus Registry Behaviour (Priority: P2)

A developer inspecting the `/metrics` output sees the standard Prometheus Go client default metrics (Go runtime stats, process metrics) alongside the application metrics, allowing out-of-the-box process-level visibility without additional instrumentation.

**Why this priority**: One of the main benefits of using the official client is getting process and runtime metrics for free. Verifying this confirms the library is wired correctly.

**Independent Test**: Scrape `/metrics` and assert that `go_goroutines` and `process_cpu_seconds_total` (or equivalent default Go collector metrics) are present.

**Acceptance Scenarios**:

1. **Given** the worker is running, **When** `/metrics` is scraped, **Then** standard Go runtime metrics (`go_goroutines`, `go_memstats_alloc_bytes`, etc.) are present.
2. **Given** the worker is running, **When** `/metrics` is scraped, **Then** process-level metrics (`process_open_fds`, `process_resident_memory_bytes`, etc.) are present.

---

### User Story 3 - Other Examples Gain Metrics Endpoints (Priority: P3)

A developer running examples `01_db_producer`, `02_cdc_worker`, or any `03_topic` binary can optionally expose their existing atomic counters via a `/metrics` endpoint, using the same library pattern, to enable observability without requiring a full custom HTTP handler.

**Why this priority**: The other examples already track counters via atomics; this story extends the benefit of the migration to them. It is lower priority because the primary goal is example 04 and the others have no existing endpoint to break.

**Independent Test**: Run `01_db_producer` with a `-metrics-port` flag, scrape `/metrics`, and confirm that `db_producer_rows_total` or equivalent metric appears.

**Acceptance Scenarios**:

1. **Given** `01_db_producer` is run with a metrics port flag, **When** `/metrics` is scraped, **Then** counter metrics reflecting rows written and bytes produced are present.
2. **Given** `02_cdc_worker` is run with a metrics port flag, **When** `/metrics` is scraped, **Then** `processed`, `skipped`, and `errors` counters are exposed.
3. **Given** the metrics port flag is absent, **When** the binary starts, **Then** no HTTP server is started and no port is bound.

---

### Edge Cases

- What happens when the worker is scrapped before any tasks have been processed (all counters at zero)?
- How does the `/metrics` handler behave under concurrent scrapes?
- What happens if the metrics port is already in use at startup?
- How does the `coordinator_up` gauge behave if the worker goroutine panics or exits?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The `/metrics` endpoint in `04_coordinated_table` MUST expose the following metrics, preserving their existing names, types, and `worker_id` label: `coordinator_tasks_processed_total` (counter), `coordinator_tasks_locked_total` (counter), `coordinator_tasks_errors_total` (counter), `coordinator_partitions_owned` (gauge), `coordinator_up` (gauge).
- **FR-002**: Metrics MUST be registered with a dedicated per-worker Prometheus registry (not the global `prometheus.DefaultRegisterer`) so that multiple workers in the same process do not conflict.
- **FR-003**: The HTTP handler serving `/metrics` MUST be generated by the Prometheus client library (`promhttp.HandlerFor`) rather than hand-written format strings.
- **FR-004**: `github.com/prometheus/client_golang` MUST appear as a direct dependency in `go.mod` (not merely an indirect one).
- **FR-005**: The `Stats` struct (or its replacement) MUST update Prometheus metric objects directly on each state change, eliminating the separate atomic counter fields currently used only to feed the hand-rolled formatter.
- **FR-006**: The default Go runtime and process collectors MUST be included in the per-worker registry so that standard runtime metrics are automatically available.
- **FR-007**: The `/metrics` endpoint MUST return HTTP 404 for any path other than `/metrics`, preserving the existing behaviour.
- **FR-008**: The `display.go` periodic logging MUST continue to work — reading values from Prometheus metrics (via their `Desc`/`Collect` mechanism, or by keeping read-only access) rather than separate atomics, OR the atomics are retained alongside the Prometheus metrics exclusively for the display function if direct read access is simpler.
- **FR-009**: The metrics port MUST remain configurable via the existing `-metrics-port` CLI flag (default 9090).
- **FR-010**: Examples `01_db_producer`, `02_cdc_worker`, and `03_topic` MAY gain Prometheus-backed metrics endpoints following the same pattern; this is optional and gated on the same `-metrics-port` flag pattern.

### Key Entities

- **Worker Registry**: A `prometheus.Registry` instance scoped to one worker, holding all application metrics for that worker.
- **Metric Descriptors**: The five named metrics (`coordinator_tasks_processed_total`, etc.) defined as `prometheus.CounterVec` / `prometheus.GaugeVec` with `worker_id` label.
- **Metrics Handler**: The `http.Handler` produced by `promhttp.HandlerFor(registry, ...)` that serialises all registered metrics on demand.
- **Stats**: The struct (or its replacement) that holds references to the Prometheus metric objects and exposes increment/set methods called from worker logic.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All five existing metric names and types are present and carry correct values when scraped, with zero changes required to any existing Unified Agent or Prometheus scrape configuration.
- **SC-002**: At least 10 standard Go runtime and process metrics (e.g., goroutine count, heap allocation, open file descriptors) appear in the `/metrics` response without any additional instrumentation code.
- **SC-003**: The hand-written Prometheus text format code in `metrics.go` is fully removed; the file either disappears or contains only library-backed handler wiring.
- **SC-004**: `go mod tidy` produces a `go.mod` where `github.com/prometheus/client_golang` is listed as a direct `require` entry (not under `// indirect`).
- **SC-005**: Scraping `/metrics` under the same load conditions as today completes in under 5 ms (matching the existing scrape-latency contract in `specs/005-04-autoscale-deploy/contracts/metrics-endpoint.md`).
- **SC-006**: The periodic display log continues to emit correct counter values aligned with the values reported by the `/metrics` endpoint.

## Assumptions

- `github.com/prometheus/client_golang v1.23.2` is already an indirect dependency; the version will be promoted to direct without a version bump unless a newer version is needed.
- The worker is always single-process; a per-worker registry (one per `Stats` instance) is sufficient — no multi-process pushgateway is required.
- The `display.go` logging function reads counter values; the simplest approach is to retain read-only atomic mirrors alongside the Prometheus counters if `prometheus.Counter` does not expose a direct `Value()` method without test helpers.
- Unified Agent's `metrics_pull` input already handles standard Prometheus text exposition format; no changes to the monitoring pipeline are required.
- Out-of-scope: adding histogram or summary metrics, adding new metric dimensions, or changing the scrape port default.
