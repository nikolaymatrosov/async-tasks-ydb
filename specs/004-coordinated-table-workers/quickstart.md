# Quickstart: Coordinated Table Workers

**Feature**: 004-coordinated-table-workers

## Prerequisites

- Go 1.26+
- Running YDB instance (local Docker or YDB Serverless)
- Migrations applied via goose

## 1. Apply migrations

```bash
goose -dir ./migrations ydb "grpc://localhost:2136/local?go_query_mode=scripting&go_fake_tx=scripting&go_query_bind=declare,numeric" up
```

## 2. Run the producer

Start inserting tasks into the `coordinated_tasks` table:

```bash
YDB_ANONYMOUS_CREDENTIALS=1 go run ./04_coordinated_table/ \
  --endpoint grpc://localhost:2136 \
  --database /local \
  --mode producer \
  --rate 100
```

## 3. Run consumer workers

Start 8 workers (in separate terminals or with `&`):

```bash
for i in $(seq 1 8); do
  YDB_ANONYMOUS_CREDENTIALS=1 go run ./04_coordinated_table/ \
    --endpoint grpc://localhost:2136 \
    --database /local \
    --mode worker \
    --partitions 256 \
    --coordination-path /local/04_coordinated_table &
done
```

## 4. Observe rebalancing

- Kill one worker (Ctrl+C or `kill`). Remaining workers pick up its partitions within ~5s.
- Start a new worker. Existing workers release excess partitions to share with the newcomer.

## Expected slog output (worker)

```json
{"time":"...","level":"INFO","msg":"worker started","worker_id":"abc-123","partitions_owned":32}
{"time":"...","level":"INFO","msg":"task locked","worker_id":"abc-123","partition_id":42,"task_id":"...","priority":200}
{"time":"...","level":"INFO","msg":"task completed","worker_id":"abc-123","partition_id":42,"task_id":"..."}
{"time":"...","level":"INFO","msg":"rebalancing","worker_id":"abc-123","old_count":32,"new_count":37,"reason":"worker_left"}
```

## Expected stats block (stdout, periodic)

```
=== Worker abc-123 Stats ===
Partitions owned:     32
Tasks processed:     147
Tasks locked:          3
Avg processing time: 102ms
Uptime:              45s
========================
```

## CLI Flags

| Flag                 | Default                       | Description                          |
|----------------------|-------------------------------|--------------------------------------|
| `--endpoint`         | (required)                    | YDB gRPC endpoint                    |
| `--database`         | (required)                    | YDB database path                    |
| `--mode`             | (required)                    | `producer` or `worker`               |
| `--partitions`       | `256`                         | Number of logical partitions         |
| `--coordination-path`| `<database>/04_coordinated_table` | Coordination node path           |
| `--rate`             | `100`                         | Producer: tasks per second           |
| `--lock-duration`    | `5s`                          | Worker: lock expiry duration         |
| `--backoff-min`      | `50ms`                        | Worker: initial backoff on empty poll|
| `--backoff-max`      | `5s`                          | Worker: max backoff on empty poll    |

## Environment Variables

| Variable              | Description                                     |
|-----------------------|-------------------------------------------------|
| `YDB_ENDPOINT`        | Alternative to `--endpoint` flag                |
| `YDB_SA_KEY_FILE`     | Path to service account key file (cloud auth)   |
| `YDB_ANONYMOUS_CREDENTIALS` | Set to `1` for local unauthenticated access |
