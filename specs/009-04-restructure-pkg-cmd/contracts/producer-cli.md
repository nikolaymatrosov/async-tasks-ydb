# CLI Contract: producer binary

**Binary**: `cmd/producer/producer`  
**Purpose**: Inserts tasks into YDB `coordinated_tasks` at a controlled rate.

## Flags

| Flag | Type | Default | Source | Description |
|---|---|---|---|---|
| `--endpoint` | string | `$YDB_ENDPOINT` | env/flag | YDB gRPC endpoint (required) |
| `--database` | string | `$YDB_DATABASE` | env/flag | YDB database path (required) |
| `--partitions` | int | `256` | flag | Number of logical task partitions |
| `--coordination-path` | string | `<database>/04_coordinated_table` | flag | Coordination node path (unused by producer; kept for parity) |
| `--rate` | int | `100` | flag | Target tasks per second |
| `--batch-window` | duration | `100ms` | flag | Batch accumulation window |
| `--report-interval` | duration | `5s` | flag | Throughput reporting interval |
| `--metrics-port` | int | `9090` | flag | Prometheus `/metrics` port |

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `YDB_ENDPOINT` | if `--endpoint` absent | YDB gRPC endpoint |
| `YDB_DATABASE` | if `--database` absent | YDB database path |
| `YDB_SA_KEY_FILE` | No | Path to service account key (takes priority) |
| `YDB_ANONYMOUS_CREDENTIALS` | No | Set to `1` for anonymous auth |

## Exit Behaviour

| Condition | Exit code | Log |
|---|---|---|
| `--endpoint` missing | 1 | `slog.Error("--endpoint or YDB_ENDPOINT is required")` |
| `--database` missing | 1 | `slog.Error("--database is required")` |
| `SIGTERM` / `SIGINT` | 0 | `slog.Info("producer stopping", "total_inserted", ...)` |

## Rejected Flags (must cause "unknown flag" error)

- `--mode` (removed per FR-006)
- `--lock-duration`, `--backoff-min`, `--backoff-max` (worker-only)
