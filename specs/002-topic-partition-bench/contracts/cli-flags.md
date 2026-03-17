# CLI Contract: `go run ./03_topic/`

**Branch**: `002-topic-partition-bench` | **Date**: 2026-03-16

## Invocation

```
go run ./03_topic/ [flags]
```

## Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `YDB_ENDPOINT` | YDB gRPC endpoint (full URL including database path) | `grpcs://ydb.serverless.yandexcloud.net:2135/ru-central1/b1g.../etn...` |
| `YDB_SA_KEY_FILE` | Path to Yandex Cloud service account JSON key file | `/etc/ydb/sa-key.json` |

Missing variables cause an immediate `slog.Error` and `os.Exit(1)`.

## Command-Line Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-users` | `int` | `100` | Number of distinct user IDs in the sampler pool |
| `-messages` | `int` | `100000` | Total messages to generate and publish per topic |
| `-topic-user` | `string` | `tasks/by_user` | Topic path (relative to DB root) for user-partitioned topic |
| `-topic-id` | `string` | `tasks/by_message_id` | Topic path (relative to DB root) for message-ID-partitioned topic |

### Validation Rules

- `-users` must be ≥ 1; program exits with `slog.Error` if ≤ 0
- `-messages` must be ≥ 1; program exits with `slog.Error` if ≤ 0
- Topic paths are used as-is after prepending the database name prefix (`db.Name() + "/" + flag`)

## Output Contract

### Structured Logs (stderr or stdout — JSON via slog)

All operational events are logged as JSON objects to the default slog handler:

```json
{"time":"...","level":"INFO","msg":"producer started","topic":"...","partitions":10}
{"time":"...","level":"INFO","msg":"publish complete","topic":"...","messages":100000}
{"time":"...","level":"INFO","msg":"scenario started","scenario":"by_user → stats"}
{"time":"...","level":"INFO","msg":"scenario complete","scenario":"by_user → stats","messages":100000,"tli_errors":12,"duration_s":45.2}
```

### Final Comparison Table (stdout — plain text)

Printed after all 4 scenarios complete. Format is fixed-width Unicode box-drawing:

```
┌──────────────────────────────┬──────────┬────────────┬──────────┬─────────┐
│ Scenario                     │ Messages │ TLI Errors │ Duration │ msg/sec │
├──────────────────────────────┼──────────┼────────────┼──────────┼─────────┤
│ by_user → stats              │ 100000   │ 12         │ 45.2s    │ 2212    │
│ by_user → processed          │ 100000   │ 0          │ 12.1s    │ 8264    │
│ by_message_id → stats        │ 100000   │ 8743       │ 127.3s   │ 785     │
│ by_message_id → processed    │ 100000   │ 0          │ 11.8s    │ 8474    │
└──────────────────────────────┴──────────┴────────────┴──────────┴─────────┘
```

Column widths are fixed; values are right-padded with spaces to fill columns.

## Exit Codes

| Code | Condition |
|------|-----------|
| `0` | All 4 scenarios completed successfully |
| `1` | Startup error (missing env vars, YDB connection failure, missing topic/table) |
| `1` | Fatal error during produce or consume phase |
| Interrupt (`SIGINT`/`SIGTERM`) | Graceful shutdown; partial results are not printed |
