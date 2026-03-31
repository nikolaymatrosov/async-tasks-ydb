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
    locked_until Timestamp,            -- expiry for stale lock recovery
    scheduled_at Timestamp,            -- optional future execution time
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

### 2. Run the producer

```bash
YDB_ANONYMOUS_CREDENTIALS=1 go run ./04_coordinated_table/ \
  --endpoint grpc://localhost:2136 \
  --database /local \
  --mode producer \
  --rate 100
```

### 3. Run workers

Start multiple workers in separate terminals:

```bash
YDB_ANONYMOUS_CREDENTIALS=1 go run ./04_coordinated_table/ \
  --endpoint grpc://localhost:2136 \
  --database /local \
  --mode worker
```

### 4. Observe rebalancing

- Kill a worker (Ctrl+C). Remaining workers pick up its partitions within ~5s.
- Start a new worker. Existing workers release excess partitions to share with the newcomer.

## CLI Flags

| Flag                  | Default                           | Description                           |
|-----------------------|-----------------------------------|---------------------------------------|
| `--endpoint`          | `$YDB_ENDPOINT`                   | YDB gRPC endpoint (required)          |
| `--database`          | —                                 | YDB database path (required)          |
| `--mode`              | —                                 | `producer` or `worker` (required)     |
| `--partitions`        | `256`                             | Number of logical partitions          |
| `--coordination-path` | `<database>/04_coordinated_table` | Coordination node path                |
| `--rate`              | `100`                             | Producer: tasks per second            |
| `--lock-duration`     | `5s`                              | Worker: lock expiry duration          |
| `--backoff-min`       | `50ms`                            | Worker: initial backoff on empty poll |
| `--backoff-max`       | `5s`                              | Worker: max backoff on empty poll     |

## Environment Variables

| Variable                    | Description                                   |
|-----------------------------|-----------------------------------------------|
| `YDB_ENDPOINT`              | Alternative to `--endpoint` flag              |
| `YDB_SA_KEY_FILE`           | Path to service account key file (cloud auth) |
| `YDB_ANONYMOUS_CREDENTIALS` | Set to `1` for local unauthenticated access   |

## File Structure

| File            | Purpose                                                              |
|-----------------|----------------------------------------------------------------------|
| `main.go`       | Entry point, flag parsing, YDB connection, coordination node setup   |
| `producer.go`   | Task insertion with murmur3 hash routing and random priority         |
| `worker.go`     | Per-partition task polling, locking, completion                      |
| `rebalancer.go` | Semaphore-based partition acquisition, membership watch, rebalancing |
| `display.go`    | Periodic stats output (structured JSON + plain text)                 |
| `utils.go`      | UUID generation helper                                               |
