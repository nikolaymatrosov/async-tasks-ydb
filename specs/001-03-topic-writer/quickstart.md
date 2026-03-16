# Quickstart: Example 03 — Direct Topic Writer

## Prerequisites

- Go 1.26+
- A running YDB instance with a service-account key file
- The YDB CLI (`ydb`) available on your PATH (for one-time topic creation)

## One-time Setup: Create the Target Topic

The example writes to a topic that does **not** exist by default. Create it once:

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
| `YDB_SA_KEY_FILE` | Yes | Path to a JSON service-account key file |

## Run the Example

```bash
export YDB_ENDPOINT="grpcs://ydb.example.com:2135"
export YDB_SA_KEY_FILE="/path/to/sa_key.json"

go run ./03_topic/
```

### Optional Flags

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `-topic` | `tasks/direct` | Topic path relative to the database root |
| `-messages` | `10` | Number of messages to publish per key |

### Example with Custom Flags

```bash
go run ./03_topic/ -topic tasks/my-topic -messages 5
```

## Expected Output

```text
2026/03/16 12:00:00 Producer started: topic=/ru-central1/.../tasks/direct partitions=3
2026/03/16 12:00:00 [key=user-42] msg 1 → partition 1 ✓
2026/03/16 12:00:00 [key=user-42] msg 2 → partition 1 ✓
2026/03/16 12:00:00 [key=order-99] msg 1 → partition 0 ✓
...
2026/03/16 12:00:01 Producer stopped.

--- Stats ---
Messages written : 20
Keys used        : 2
Partitions used  : 2
```

All messages with `key=user-42` consistently land on **partition 1**; all messages with `key=order-99` land on **partition 0** — demonstrating deterministic key-to-partition routing.

## How It Works

1. **Startup** — `Producer.Start()` calls `db.Topic().Describe()` to get the live list of active partitions, then opens one `topicwriter.Writer` per partition with `WithWriterPartitionID` and `WithWriterWaitServerAck(true)`.
2. **Routing** — `Producer.Write(ctx, key, messages...)` hashes `key` with murmur3 32-bit, maps the result to a partition index, and forwards to that partition's `safeWriter`.
3. **Retry** — `safeWriter.Write` loops with exponential backoff (1 s → 30 s max, 5 min total) on transport errors; loops indefinitely on queue-full; surfaces permanent errors immediately.
4. **Shutdown** — `Producer.Stop()` closes all partition writers and joins any close errors.

## Troubleshooting

| Symptom | Likely cause | Fix |
| ------- | ------------ | --- |
| `topic not found` | Topic not created | Run the setup command above |
| `YDB_ENDPOINT is not set` | Missing env var | Export the variable |
| All messages land on partition 0 | Topic has only 1 partition | Recreate topic with `--partitions-count 3` |
| `failed to start writer` on startup | Auth error | Verify `YDB_SA_KEY_FILE` path and permissions |
