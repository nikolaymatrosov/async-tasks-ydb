# Feature Specification: Worker Task Processor — API Gateway Call

**Feature Branch**: `010-yc-apigw-dummy`
**Created**: 2026-04-24
**Status**: Draft

## Clarifications

### Session 2026-04-24

- Q: HTTP method for API Gateway call → A: POST; task payload JSON sent as request body
- Q: Terraform provisioning scope → A: API Gateway in a separate Terraform module (infrastructure concern, not Go worker scope)

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Inject real work into task processing (Priority: P1)

An operator running the coordinated-table worker experiment wants each locked
task to perform a real, observable action rather than a no-op sleep. The
producer embeds the API Gateway URL inside the task payload as JSON; the worker
reads that URL from the payload, then makes an HTTP POST request to it —
sending the full task payload JSON as the request body — before marking the
task `completed`.

**Why this priority**: Without real work the experiment produces no meaningful
latency or error-rate data. This is the sole purpose of the feature.

**Independent Test**: Produce tasks whose JSON payload contains a valid URL,
run the worker, and observe that each task completion is preceded by a
successful HTTP POST to the URL stored in that task's payload, with the payload
JSON as the request body.

**Acceptance Scenarios**:

1. **Given** a task whose payload contains a valid URL,
   **When** the worker processes it,
   **Then** it makes one HTTP POST request to that URL, with the task payload
   JSON as the body, before completing the task.

2. **Given** the HTTP POST returns `{"status":"ok"}` with HTTP 200,
   **When** the task action finishes,
   **Then** the task is marked `completed` in the database.

3. **Given** the HTTP POST returns a non-200 status or network error,
   **When** the task action finishes,
   **Then** the error is logged, the task is NOT marked `completed`, and the
   worker's error counter increments.

4. **Given** a task whose payload JSON is malformed or missing the URL field,
   **When** the worker attempts to process it,
   **Then** the error is logged and the task is NOT marked `completed`.

---

### User Story 2 — Observe per-task HTTP call outcomes in logs (Priority: P2)

An operator wants to trace which tasks succeeded or failed their HTTP call, using
structured log output, without inspecting the database directly.

**Why this priority**: Observability of the real work is necessary to validate
the experiment; the task status alone is insufficient.

**Independent Test**: Run the worker, capture stdout, and confirm log lines
contain task ID, HTTP status code (or error), and outcome.

**Acceptance Scenarios**:

1. **Given** a task completes its HTTP call successfully,
   **When** the log is inspected,
   **Then** a structured log line contains `task_id`, `http_status=200`, and
   `outcome=completed`.

2. **Given** a task's HTTP call fails,
   **When** the log is inspected,
   **Then** a structured log line contains `task_id`, the error or HTTP status,
   and `outcome=error`.

---

### Edge Cases

- What happens when the API Gateway endpoint is temporarily unreachable (timeout)?
  The task action must propagate the error rather than silently succeed.
- What happens when the context is cancelled mid-HTTP-call?
  The HTTP request must honour context cancellation and not block shutdown.
- What happens if the response body is unreadable?
  The error should be treated the same as a non-200 response.
- What happens if the task payload is not valid JSON?
  Treated as a processing error — task stays `locked`, error counter increments.
- What happens if the payload JSON has no `url` field?
  Treated as a processing error — task stays `locked`, error counter increments.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The task producer MUST serialize each task's payload as a JSON
  object containing at minimum a `url` field whose value is the API Gateway
  endpoint URL to call for that task.
- **FR-002**: The `Worker` MUST expose a configurable task-processor function
  field (or equivalent injection point) that is called once per locked task,
  receiving the task's raw payload, before completion.
- **FR-003**: The task-processor MUST unmarshal the task payload JSON and
  extract the `url` field; a missing or malformed field MUST be treated as a
  processing error.
- **FR-004**: The task-processor MUST make a single HTTP POST request to the URL
  extracted from the payload, with the full task payload JSON as the request
  body and `Content-Type: application/json`.
- **FR-005**: The HTTP request MUST carry the task ID as the `X-Task-ID` request
  header so each call is identifiable in API Gateway logs.
- **FR-006**: A non-200 HTTP response, transport error, or payload parse error
  MUST cause the task to remain in `locked` state (not be moved to `completed`)
  and increment the worker's error counter.
- **FR-007**: A successful HTTP response (HTTP 200) MUST allow the task to
  proceed to `completed` state.
- **FR-008**: The task-processor invocation MUST respect the task's processing
  context, including cancellation signals.
- **FR-009**: Each HTTP call outcome MUST be recorded in a structured log entry
  containing at minimum: `task_id`, `url`, `http_status` (or error string), and
  final task outcome.
- **FR-010**: The worker MUST expose a Prometheus counter metric
  `coordinator_apigw_calls_total` with a label `http_status` whose value is the
  HTTP status code as a string (e.g., `"200"`, `"503"`), or `"error"` for
  transport-level failures. This metric MUST also carry the existing
  `worker_id` label.

### Key Entities

- **Task Payload**: A JSON object stored in the `payload` column of
  `coordinated_tasks`. Must contain a `url` string field. The producer is
  responsible for populating it; the worker reads it at processing time and
  sends it verbatim as the POST body.
- **Task Processor**: A function called with task context and the raw payload
  string before the task is marked `completed`. Unmarshals the payload, POSTs
  it to the extracted URL, and returns an error if anything fails.
- **API Gateway Endpoint**: The HTTP endpoint called per task via POST. URL is
  embedded in the task payload. Currently returns `{"status":"ok"}` with HTTP
  200. Provisioned by a dedicated Terraform module (infrastructure concern).
- **API Gateway Call Metric**: A labelled counter tracking every HTTP call
  attempt, broken down by `http_status` (status code string or `"error"`) and
  `worker_id`. Enables alerting and dashboards on non-200 response trends.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every task processed by the worker results in exactly one HTTP
  POST to the API Gateway endpoint, verifiable via API Gateway access logs.
- **SC-002**: A task whose HTTP call fails is never marked `completed` in the
  database — verifiable by querying `coordinated_tasks` after a forced failure.
- **SC-003**: A task with a malformed or URL-less payload is never marked
  `completed` — verifiable by inspecting `coordinated_tasks.status` after
  intentionally producing such a task.
- **SC-004**: The worker handles context cancellation during an in-flight HTTP
  call without hanging, completing shutdown within the configured backoff window.
- **SC-005**: The `coordinator_apigw_calls_total` metric counter for
  `http_status="200"` increases by exactly one per successfully processed task,
  verifiable by scraping the `/metrics` endpoint after a known task batch.

## Assumptions

- The API Gateway endpoint accepts HTTP POST with `Content-Type: application/json`
  and a JSON body; it returns HTTP 200 for any well-formed request.
- The `coordinated_tasks` table schema and lock/complete flow remain unchanged;
  the existing `payload Utf8 NOT NULL` column is used as-is.
- The producer already writes to `coordinated_tasks`; only the payload content
  changes (it must now be valid JSON with a `url` field).
- No retry logic is required for failed HTTP calls in this experiment phase;
  the task simply stays `locked` and will be retried on the next poll cycle
  when the lock expires.
- The existing `Worker` struct in `04_coordinated_table/pkg/taskworker/worker.go`
  is the primary integration point; the producer code is updated in the same
  package/binary.
- The API Gateway resource is managed by a separate Terraform module; its
  provisioned URL is passed to the producer via the `--apigw-url` flag /
  `APIGW_URL` environment variable.
