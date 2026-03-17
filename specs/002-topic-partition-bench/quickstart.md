# Quickstart: Topic Partition Benchmark

**Branch**: `002-topic-partition-bench` | **Date**: 2026-03-16

## Prerequisites

- Go 1.26+
- A running YDB instance (Yandex Cloud Serverless or dedicated)
- A service account key JSON file with `ydb.viewer` + `ydb.editor` roles
- `goose` CLI installed: `go install github.com/pressly/goose/v3/cmd/goose@latest`

## Step 1: Set Environment Variables

```bash
export YDB_ENDPOINT="grpcs://ydb.serverless.yandexcloud.net:2135/ru-central1/<folder>/<db>"
export YDB_SA_KEY_FILE="/path/to/sa-key.json"
```

## Step 2: Run Migrations

Apply all migrations including the new benchmark infrastructure:

```bash
make migrate-up
# or directly:
goose -dir migrations ydb "$YDB_ENDPOINT" up
```

This creates:
- `tasks/by_user` — 10-partition topic (consumers: `bench-byuser-stats`, `bench-byuser-processed`)
- `tasks/by_message_id` — 10-partition topic (consumers: `bench-bymsgid-stats`, `bench-bymsgid-processed`)
- `stats` table — per-user counters
- `processed` table — message ID log

## Step 3: Run the Benchmark

Default run (100 users, 100,000 messages):

```bash
go run ./03_topic/
```

Custom run:

```bash
go run ./03_topic/ -users 50 -messages 10000
```

## Expected Output

Structured JSON logs stream to stdout during the run. After all 4 scenarios complete, a comparison
table is printed:

```
┌──────────────────────────────┬──────────┬────────────┬──────────┬─────────┐
│ Scenario                     │ Messages │ TLI Errors │ Duration │ msg/sec │
├──────────────────────────────┼──────────┼────────────┼──────────┼─────────┤
│ by_user → stats              │ 100000   │ ~10s       │ ~45s     │ ~2200   │
│ by_user → processed          │ 100000   │ 0          │ ~12s     │ ~8300   │
│ by_message_id → stats        │ 100000   │ ~8000s     │ ~130s    │ ~770    │
│ by_message_id → processed    │ 100000   │ 0          │ ~12s     │ ~8300   │
└──────────────────────────────┴──────────┴────────────┴──────────┴─────────┘
```

## Verifying Correctness

After the benchmark completes, verify the stats table integrity:

```sql
SELECT SUM(a) + SUM(b) + SUM(c) AS total FROM stats;
-- Should equal your -messages value (default 100000)
```

## Key Acceptance Signals

| Signal | Pass Condition |
|--------|---------------|
| `by_user → stats` TLI count | **≥10× lower** than `by_message_id → stats` (SC-001) |
| Both `→ processed` TLI counts | **0 or near-zero** (SC-002) |
| All Messages columns | **Equal to `-messages` flag value** (SC-003) |
| Stats sum query | **Equal to `-messages` flag value** (SC-004) |
| Table printed | **No manual intervention** required (SC-005) |

## Teardown

```bash
make migrate-down
# or:
goose -dir migrations ydb "$YDB_ENDPOINT" down-to 20260316000003
```

This drops `tasks/by_user`, `tasks/by_message_id`, `stats`, and `processed`.
