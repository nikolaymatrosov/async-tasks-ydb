# 04 Coordinated Table Workers

Distributed task processing over a YDB table using coordination-node semaphores for partition ownership and dynamic rebalancing.

## How It Works

A **producer** inserts task rows into the `coordinated_tasks` table. Each task is assigned to one of 256 logical partitions via a murmur3 hash of its ID. **Workers** connect to a YDB coordination node, compete for exclusive partition semaphores, and process tasks from their owned partitions.

```text
                 coordinated_tasks table
                 (256 logical partitions)
                          |
         +----------------+----------------+
         |                |                |
   partition-0..85   partition-86..170  partition-171..255
         |                |                |
      Worker A         Worker B         Worker C
```

### Partition Ownership

Each of the 256 partitions maps to an exclusive ephemeral semaphore on the coordination node (`partition-0` ... `partition-255`). Workers acquire semaphores greedily on startup, capped at `ceil(256 / active_workers)` per worker. A shared `worker-registry` semaphore tracks active membership.

### Rebalancing

When a worker joins or leaves (detected via `DescribeSemaphore` watch), every worker recalculates its target capacity and releases excess partitions. Released semaphores are immediately available for other workers to acquire. Session loss triggers automatic reconnection and re-acquisition.

### Task Processing

Within each owned partition, the worker polls for the highest-priority eligible task:

- **Eligible**: `status = 'pending'` or `status = 'locked'` with expired `locked_until` (stale lock recovery)
- **Scheduled**: tasks with `scheduled_at` in the future are skipped until that time
- **Locking**: a serializable read-write transaction selects the task and sets a random `lock_value` + `locked_until`
- **Completion**: simulated 100ms work, then marked `completed` with `done_at`
- **Backoff**: exponential backoff (50ms to 5s) on empty polls, reset when a task is found

## Table Schema

```sql
CREATE TABLE coordinated_tasks (
    id           Utf8       NOT NULL,
    hash         Int64      NOT NULL,
    partition_id Uint16     NOT NULL,
    priority     Uint8      NOT NULL,  -- 0-255, higher = more urgent
    status       Utf8       NOT NULL,  -- pending | locked | completed
    payload      Utf8       NOT NULL,
    lock_value   Utf8,                 -- random UUID set on lock
    locked_until Timestamp,           -- expiry for stale lock recovery
    scheduled_at Timestamp,           -- optional future execution time
    created_at   Timestamp  NOT NULL,
    done_at      Timestamp,
    PRIMARY KEY (partition_id, priority, id)
);
```

The composite primary key `(partition_id, priority, id)` enables efficient per-partition scans ordered by priority descending.

## Quickstart

### 1. Apply migrations

```bash
goose -dir ./migrations ydb \
  "grpc://localhost:2136/local?go_query_mode=scripting&go_fake_tx=scripting&go_query_bind=declare,numeric" up
```

### 2. Build both binaries

```bash
# From repo root
go build -o /dev/null ./04_coordinated_table/cmd/producer/
go build -o /dev/null ./04_coordinated_table/cmd/worker/
```

### 3. Run the producer

```bash
YDB_ANONYMOUS_CREDENTIALS=1 go run ./04_coordinated_table/cmd/producer/ \
  --endpoint grpc://localhost:2136 \
  --database /local \
  --rate 100
```

### 4. Run workers

Start multiple workers in separate terminals:

```bash
YDB_ANONYMOUS_CREDENTIALS=1 go run ./04_coordinated_table/cmd/worker/ \
  --endpoint grpc://localhost:2136 \
  --database /local
```

### 5. Observe rebalancing

- Kill a worker (Ctrl+C). Remaining workers pick up its partitions within ~5s.
- Start a new worker. Existing workers release excess partitions to share with the newcomer.

## CLI Flags

### Producer (`cmd/producer/`)

| Flag | Default | Description |
| --- | --- | --- |
| `--endpoint` | `$YDB_ENDPOINT` | YDB gRPC endpoint (required) |
| `--database` | `$YDB_DATABASE` | YDB database path (required) |
| `--partitions` | `256` | Number of logical partitions |
| `--coordination-path` | `<database>/04_coordinated_table` | Coordination node path (unused; kept for parity) |
| `--rate` | `100` | Target tasks per second |
| `--batch-window` | `100ms` | Batch accumulation window |
| `--report-interval` | `5s` | Throughput reporting interval |
| `--metrics-port` | `9090` | Prometheus `/metrics` port |

### Worker (`cmd/worker/`)

| Flag | Default | Description |
| --- | --- | --- |
| `--endpoint` | `$YDB_ENDPOINT` | YDB gRPC endpoint (required) |
| `--database` | `$YDB_DATABASE` | YDB database path (required) |
| `--partitions` | `256` | Number of logical partitions |
| `--coordination-path` | `<database>/04_coordinated_table` | Coordination node path |
| `--lock-duration` | `5s` | Lock expiry duration per task |
| `--backoff-min` | `50ms` | Initial backoff on empty poll |
| `--backoff-max` | `5s` | Maximum backoff on empty poll |
| `--metrics-port` | `9090` | Prometheus `/metrics` port |

## Environment Variables

| Variable | Description |
| --- | --- |
| `YDB_ENDPOINT` | Alternative to `--endpoint` flag |
| `YDB_SA_KEY_FILE` | Path to service account key file (cloud auth) |
| `YDB_ANONYMOUS_CREDENTIALS` | Set to `1` for local unauthenticated access |

## File Structure

```text
04_coordinated_table/
├── cmd/
│   ├── producer/
│   │   └── main.go        ← producer entry point (flags, ydbconn.Open, metrics, taskproducer.Produce)
│   └── worker/
│       └── main.go        ← worker entry point (flags, ydbconn.Open, CreateNode, metrics, Worker.Run)
├── pkg/
│   ├── uid/
│   │   └── uid.go         ← GenerateUUID() (string, error)
│   ├── metrics/
│   │   ├── handler.go     ← Handler(registry) http.Handler
│   │   ├── worker_stats.go ← Stats, NewStats, Display
│   │   └── producer_stats.go ← ProducerStats, NewProducerStats
│   ├── rebalancer/
│   │   └── rebalancer.go  ← Rebalancer, PartitionEvent, NewRebalancer, Start, Stop
│   ├── taskworker/
│   │   └── worker.go      ← Worker, Run
│   ├── taskproducer/
│   │   └── producer.go    ← Produce
│   └── ydbconn/
│       └── conn.go        ← Open(ctx, endpoint, database) (*ydb.Driver, error)
└── README.md
```
