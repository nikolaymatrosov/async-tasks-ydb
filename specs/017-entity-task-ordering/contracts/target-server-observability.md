# Contract: Target Server — Observability Surface

**Component**: `06_target_server/main.go`

Three endpoints, served on `--metrics-port` (default `:9091`) so observability traffic doesn't
share the ingest listener.

## `GET /healthz`

Always `200 OK` body `{"status":"ok"}` while the process is past startup. Returns `503
Service Unavailable` during graceful shutdown (after `signal.NotifyContext` fires, before
`srv.Shutdown` returns).

## `GET /state`

Operator-facing JSON snapshot. Bounded by an optional `?top=<int>` query parameter (default 50,
max 1000) to cap response size when entity count is high.

```json
{
  "config": {
    "fault_429_percent": 25,
    "fault_5xx_percent": 10,
    "listen": ":8080"
  },
  "totals": {
    "accepted_total": 18234,
    "duplicate_total": 91,
    "violation_total": 0,
    "fault_429_total": 4523,
    "fault_5xx_total": 1810
  },
  "top_entities": [
    {"entity_id": "entity-0000042", "last_accepted_seq": 73, "accepted": 73, "last_accepted_at": "2026-04-29T11:02:14Z"},
    ...
  ]
}
```

`top_entities` is ordered by `last_accepted_at` descending (most-recently-active first).

This endpoint satisfies FR-020: operators can observe current configured fault rates, accepted
counts per entity, and totals through the server's observability surface.

## `GET /metrics`

Prometheus exposition (text/plain `version=0.0.4`). Series:

```
# HELP target_server_accepted_total Events accepted in submission order, by entity bucket.
# TYPE target_server_accepted_total counter
target_server_accepted_total{bucket="0"} 412
target_server_accepted_total{bucket="1"} 387
... (64 buckets, see research §10)

# HELP target_server_duplicate_total Idempotent duplicate redeliveries (FR-017), by bucket.
# TYPE target_server_duplicate_total counter
target_server_duplicate_total{bucket="0"} 5
...

# HELP target_server_ordering_violation_total Out-of-order arrivals (FR-016), by bucket.
# TYPE target_server_ordering_violation_total counter
target_server_ordering_violation_total{bucket="0"} 0
...

# HELP target_server_fault_injected_total Requests answered with an injected fault, by status.
# TYPE target_server_fault_injected_total counter
target_server_fault_injected_total{status="429"} 4523
target_server_fault_injected_total{status="503"} 1810

# HELP target_server_request_duration_seconds Ingest handler latency.
# TYPE target_server_request_duration_seconds histogram
target_server_request_duration_seconds_bucket{le="0.001"} ...
...

# HELP target_server_fault_percent Currently configured fault rates.
# TYPE target_server_fault_percent gauge
target_server_fault_percent{kind="429"} 25
target_server_fault_percent{kind="5xx"} 10
```

**Cardinality**: 64-bucket label keeps Prometheus series bounded (research §10) regardless of
entity count.

## Stats block on shutdown

On `SIGTERM`/`SIGINT`, after the HTTP server stops accepting new connections, the process prints
a single fixed-format block to stdout (per constitution V):

```
=== target server stats ===
uptime              :  00h 12m 47s
fault_429_percent   :  25
fault_5xx_percent   :  10
accepted_total      :  18234
duplicate_total     :  91
violation_total     :  0
fault_429_total     :  4523
fault_5xx_total     :  1810
unique_entities     :  1000
===========================
```

This is the human-readable acceptance surface for the spec's quickstart scenarios.
