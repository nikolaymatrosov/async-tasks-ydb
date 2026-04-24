# Research: Worker Task Processor — API Gateway Call

## 1. Payload encoding

**Decision**: JSON object `{"url":"<endpoint>"}` stored as UTF-8 in the existing `payload` column.

**Rationale**: `payload Utf8 NOT NULL` already exists and is already upserted by the producer.  Switching the value from the current freeform string (`"task-payload-<uuid>"`) to a JSON object requires no schema migration.  `encoding/json` is stdlib — no new dependency.

**Alternatives considered**:

- Protobuf: smaller on the wire but requires a new dependency and a code-gen step.
- Raw URL string: simpler, but forecloses carrying additional per-task metadata later.

---

## 2. HTTP client strategy

**Decision**: Use `http.DefaultClient` with a request built via `http.NewRequestWithContext` so context cancellation is honoured.

**Rationale**: No custom TLS, redirect policy, or connection pool is needed for this experiment. `DefaultClient` is adequate. Attaching the per-task context ensures the HTTP call is cancelled when the worker shuts down or loses its coordination lease.

**Alternatives considered**:

- Per-worker `http.Client` with explicit timeout: unnecessary complexity for a single-endpoint experiment.
- Third-party HTTP client library: would add a direct dependency, violating the constitution constraint.

---

## 3. HTTP method and request body

**Decision**: HTTP POST. Request body = raw task `payload` string (already valid JSON). `Content-Type: application/json`. Task ID sent as `X-Task-ID` header.

**Rationale**: POST allows arbitrary JSON data to be forwarded to the API Gateway per request, making the endpoint useful for future experiments that need to inspect per-task data. The raw payload is sent verbatim — no re-serialisation needed, no extra allocations. Task ID stays in a header so the body schema remains free-form.

**Alternatives considered**:

- GET: no request body, ruled out when richer per-task data passing became a requirement.
- Query parameter for task ID: pollutes the canonical URL; header is cleaner for tracing metadata.

---

## 4. Metric design

**Decision**: New `*prometheus.CounterVec` named `coordinator_apigw_calls_total` with labels `worker_id` (const, matches existing metrics) and `http_status` (variable: `"200"`, `"503"`, `"error"`, etc.).

**Rationale**: Existing `Stats` counters are plain `prometheus.Counter` — no labels.  A `CounterVec` is required to differentiate response codes.  The metric is registered on the existing per-worker `prometheus.Registry` and served on the same `/metrics` endpoint.

`http_status = "error"` is used for transport-level failures (no response received).  Numeric string codes (`"200"`, `"503"`) are used for received HTTP responses, matching Prometheus community conventions for HTTP metrics.

**Alternatives considered**:

- `http_status_class` labels (`"2xx"`, `"5xx"`): less granular; hides specific error codes.
- Histogram: overkill for a simple call-count experiment.

---

## 5. ProcessTask injection point

**Decision**: Add `ProcessTask func(ctx context.Context, taskID string, payload string) error` field to `taskworker.Worker`.  The concrete implementation is defined in `cmd/worker/main.go` as a closure over `*metrics.Stats`.

**Rationale**: Keeps `taskworker` free of HTTP and metrics concerns.  The field is set before `worker.Run` is called — same pattern used by existing fields (`DB`, `Stats`).  A nil `ProcessTask` causes `completeTask` to skip processing (acts as a no-op, preserving backward compat for tests).

**Alternatives considered**:

- Interface `TaskProcessor`: heavier; unnecessary for one implementation.
- Embedding the HTTP call directly in `worker.go`: couples the package to HTTP, makes it untestable.

---

## 6. `lockNextTask` payload fetch

**Decision**: Extend the existing SELECT in `lockNextTask` to also fetch `payload`, and add `payload string` to `lockedTask`.

**Rationale**: The processor needs the payload to extract the URL.  Fetching it in the same transaction that locks the task is correct — it reads the row at the serializable isolation level, so no separate read is needed.

**No schema change required.**
