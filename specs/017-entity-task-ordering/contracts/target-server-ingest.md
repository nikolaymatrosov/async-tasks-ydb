# Contract: Target Server — Ingest Endpoint

**Component**: `06_target_server/main.go`
**Audience**: the worker in `05_ordered_tasks/cmd/worker/main.go`

## Endpoint

`POST /` (and any path — the server matches `/` as a catch-all so the worker's
`payload.url = "https://${host}/"` continues to work unchanged).

## Request

| Header | Required | Source | Notes |
|---|---|---|---|
| `Content-Type: application/json` | Yes (existing) | worker | Already set today |
| `X-Task-ID` | Yes (existing) | worker | Already set today |
| `X-Entity-ID` | **Yes (new)** | worker | Copy of `ClaimedTask.EntityID` |
| `X-Entity-Seq` | **Yes (new)** | worker | Decimal string of `ClaimedTask.EntitySeq` (uint64) |

Body: opaque JSON. The target server does not interpret it (the producer's `{"url": "..."}` is
passed through unchanged). Body size is bounded to 64 KiB; larger requests are rejected `413
Payload Too Large`.

## Behaviour

Pseudocode (in priority order):

```text
1. If body > 64 KiB              → 413 Payload Too Large; do not log violation; do not advance state.
2. If X-Entity-ID missing/empty   → 400 Bad Request {"error":"missing X-Entity-ID"}.
3. If X-Entity-Seq missing/non-numeric/zero → 400 Bad Request {"error":"invalid X-Entity-Seq"}.
4. Roll fault injection (uniform random 0..99):
     roll < fault_429              → 429 Too Many Requests; do not advance state; do not count as violation.
     roll < fault_429 + fault_5xx  → 503 Service Unavailable; same as above.
5. Else (no fault):
     Acquire per-entity lock (sharded sync.Mutex by hash(entity_id) % 64).
     last  := perEntity[entity_id].LastAcceptedSeq   (0 if absent)
     recv  := X-Entity-Seq
     if recv > last:                  // accept (entity_seq is sparse-monotonic; gaps are legitimate)
       perEntity[entity_id].LastAcceptedSeq = recv; .Accepted++; .LastAcceptedAt = now()
       counter accepted_total{bucket}++
       respond 200 OK
     elif recv == last:               // FR-017 idempotent duplicate
       respond 200 OK; counter duplicates_total{bucket}++
     else:                             // recv < last → rewind = ordering violation
       counter ordering_violation_total{bucket}++
       slog.Warn("ordering violation",
         "entity_id", entity_id, "last_accepted_seq", last, "received_seq", recv,
         "task_id", task_id, "kind", "rewind")
       respond 200 OK    // we don't want the worker to retry the violation
```

The "gap" case from a previous draft is not a violation: `entity_seq` is the topic-partition
offset and is intrinsically sparse per entity (other entities sharing the topic partition consume
offsets in between). FR-016 only requires detecting *out-of-order arrivals* — strict-greater
catches exactly the rewind case.

## Response shape

`200 OK` body:

```json
{"status":"accepted","entity_id":"<id>","entity_seq":<uint64>,"last_accepted":<uint64>}
```

(`status` ∈ `accepted`, `duplicate`, `violation`.)

`429`/`503` body:

```json
{"status":"throttled","retry_after_ms":<int>}
```

`400`/`413` bodies are JSON `{"error":"..."}` plain.

## Status code rationale (FR-021)

- `429` and `503` are both in the worker's existing transient-error path: the worker treats any
  non-200 from `apigw call` as `error` and triggers backoff via `MarkFailedWithBackoff`, which is
  exactly the path this feature wants to exercise (FR-006).
- `200` is returned for ordering violations because the violation is *not* something a retry can
  fix — counting it and continuing is the right behaviour for a test target.

## Configuration

| Flag | Env | Default | Notes |
|---|---|---|---|
| `--listen` | `LISTEN_ADDR` | `:8080` | HTTP listener |
| `--fault-429-percent` | `FAULT_429_PERCENT` | `0` | 0..100 |
| `--fault-5xx-percent` | `FAULT_5XX_PERCENT` | `0` | 0..100 |
| `--metrics-port` | `METRICS_PORT` | `9091` | Separate listener for `/metrics` (so ingest traffic doesn't share scrape port) |

Validation: `fault_429 + fault_5xx ≤ 100`; otherwise the server logs a structured error and exits 1.

## Worker-side change (minimal)

In `05_ordered_tasks/cmd/worker/main.go`, `newAPIGWProcessor` is updated to accept a
`ClaimedTask` (instead of just `taskID, payload`) so it can attach two new headers:

```go
req.Header.Set("X-Entity-ID",  task.EntityID)
req.Header.Set("X-Entity-Seq", strconv.FormatUint(task.EntitySeq, 10))
```

The function signature on `Worker.ProcessTask` is widened from `func(ctx, taskID, payload string)
error` to `func(ctx context.Context, task ClaimedTask) error`, but the call site in `worker.go`
already has the `ClaimedTask` in hand.
