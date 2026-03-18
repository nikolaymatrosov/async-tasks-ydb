# Direct Topic Writer

A YDB topic producer that routes messages to partitions deterministically using murmur3 key hashing, with exponential-backoff retry on transport errors.

## Overview

This example demonstrates:

- **Per-partition writers**: One `topicwriter.Writer` pinned to each active partition
- **Deterministic routing**: Murmur3 32-bit hash of the partition key selects the target partition
- **Resilient writes**: Exponential backoff (1 s → 30 s, 5 min total) on transport errors; indefinite retry on queue-full
- **Graceful shutdown**: All partition writers closed cleanly on Ctrl-C or demo completion

## One-time Setup: Create the Target Topic

```bash
ydb \
  --endpoint "$YDB_ENDPOINT" \
  --sa-key-file "$YDB_SA_KEY_FILE" \
  topic create \
  --partitions-count 3 \
  "$(ydb --endpoint "$YDB_ENDPOINT" --sa-key-file "$YDB_SA_KEY_FILE" scheme ls --fullpath)/tasks/direct"
```

> Adjust `--partitions-count` to match your cluster. Three partitions is enough to demonstrate multi-partition routing.

## Environment Variables

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `YDB_ENDPOINT` | Yes | YDB gRPC endpoint, e.g. `grpcs://ydb.example.com:2135` |
| `YDB_SA_KEY_FILE` | No | Path to a JSON service-account key file (if unset, VM metadata credentials are used) |

## Run

```bash
export YDB_ENDPOINT="grpcs://ydb.example.com:2135"
export YDB_SA_KEY_FILE="/path/to/sa_key.json"  # optional, omit on VM

go run ./03_topic/
```

### Flags

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `-topic` | `tasks/direct` | Topic path relative to the database root |
| `-messages` | `10` | Number of messages to publish per partition key |

### Example with Custom Flags

```bash
go run ./03_topic/ -topic tasks/my-topic -messages 5
```

## Expected Output

```text
{"time":"2026-03-16T12:00:00Z","level":"INFO","msg":"producer started","topic":"/ru-central1/.../tasks/direct","partitions":3}
{"time":"2026-03-16T12:00:00Z","level":"INFO","msg":"message written","partition_key":"user-42","partition_id":1,"msg_index":1}
{"time":"2026-03-16T12:00:00Z","level":"INFO","msg":"message written","partition_key":"user-42","partition_id":1,"msg_index":2}
{"time":"2026-03-16T12:00:00Z","level":"INFO","msg":"message written","partition_key":"order-99","partition_id":0,"msg_index":1}
...
{"time":"2026-03-16T12:00:01Z","level":"INFO","msg":"producer stopped"}

--- Stats ---
Messages written : 20
Keys used        : 2
Partitions used  : 2
```

All messages with `partition_key=user-42` consistently land on **partition 1**; all messages with `partition_key=order-99` land on **partition 0** — demonstrating deterministic key-to-partition routing.

## How It Works

1. **Startup** — `Producer.Start()` calls `db.Topic().Describe()` to get the live list of active partitions, then opens one `topicwriter.Writer` per partition with `WithWriterPartitionID` and `WithWriterWaitServerAck(true)`.
2. **Routing** — `Producer.Write(ctx, key, messages...)` hashes `key` with murmur3 32-bit, maps the result to a partition index, and forwards to that partition's `safeWriter`.
3. **Retry** — `safeWriter.Write` loops with exponential backoff (1 s → 30 s max, 5 min total) on transport errors; loops indefinitely on queue-full; surfaces permanent errors immediately.
4. **Shutdown** — `Producer.Stop()` closes all partition writers and joins any close errors.

## Dependencies

- `github.com/ydb-platform/ydb-go-sdk/v3`: YDB SDK (topic writer API)
- `github.com/ydb-platform/ydb-go-yc`: YDB authentication with service accounts
- `github.com/google/uuid`: UUID generation for task messages
- `github.com/twmb/murmur3`: Murmur3 hash for partition key routing

## Troubleshooting

| Symptom | Likely cause | Fix |
| ------- | ------------ | --- |
| `topic not found` | Topic not created | Run the setup command above |
| `YDB_ENDPOINT is not set` | Missing env var | Export the variable |
| All messages land on partition 0 | Topic has only 1 partition | Recreate topic with `--partitions-count 3` |
| `failed to start writer` on startup | Auth error | Verify `YDB_SA_KEY_FILE` path and permissions |
| Retry warnings in logs | Transient network issue | Wait — writes will succeed within 5 minutes |
