# Data Model: Worker Task Processor — API Gateway Call

## Existing schema (unchanged)

Table: `coordinated_tasks` (managed by goose migration `20260329000005`)

| Column        | Type      | Notes                                      |
|---------------|-----------|--------------------------------------------|
| id            | Utf8      | UUID, part of PK                           |
| hash          | Int64     | murmur3 of id, used for partition routing  |
| partition_id  | Uint16    | PK component                               |
| priority      | Uint8     | PK component, higher = processed first     |
| status        | Utf8      | `pending` → `locked` → `completed`        |
| payload       | Utf8      | **Was** freeform string; **now** JSON      |
| lock_value    | Utf8?     | UUID set when locked                       |
| locked_until  | Timestamp?| Expiry for the current lock                |
| scheduled_at  | Timestamp?| Earliest time task may be processed        |
| created_at    | Timestamp | Set by producer                            |
| done_at       | Timestamp?| Set when status → completed                |

**No DDL changes.**  No new migration needed.

---

## Payload JSON schema (application-level)

```json
{
  "url": "https://d5dojsm891eqrlcparqd.nkhmighe.apigw.yandexcloud.net"
}
```

- **`url`** (string, required): The HTTP endpoint the worker must GET.
- Additional fields may appear in future; the worker ignores unknown keys.
- A missing or empty `url` field is a hard error — the task stays `locked`.

---

## In-memory structures (Go)

### `lockedTask` (extended)

```
lockedTask {
    id          string   // task UUID
    partitionID uint16
    priority    uint8
    lockValue   string   // UUID used to verify ownership on update
    payload     string   // raw JSON from payload column  ← NEW
}
```

### `taskPayload` (new, internal to processor)

```
taskPayload {
    URL string `json:"url"`
}
```

Unmarshalled from `lockedTask.payload` by the task processor before the HTTP call.

---

## Metric

`coordinator_apigw_calls_total` — Prometheus CounterVec

| Label       | Values                              |
|-------------|-------------------------------------|
| `worker_id` | const per process (UUID)            |
| `http_status` | `"200"`, `"404"`, `"503"`, …, or `"error"` for transport failures |
