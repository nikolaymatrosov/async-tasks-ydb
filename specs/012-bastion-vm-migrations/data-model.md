# Data Model: Bastion VM Migrations (012)

## Schema Changes

**None.** This feature adds no new YDB tables, topics, or changefeeds.

The existing goose migration files in `migrations/` remain the source of truth for all schema objects:

| Migration | Creates |
|-----------|---------|
| `20260222000001_create_tasks_table.sql` | `tasks` table |
| `20260222000002_create_tasks_changefeed.sql` | changefeed on `tasks` |
| `20260316000003_create_direct_topic.sql` | `tasks/by_user`, `tasks/by_message_id` topics |
| `20260316000004_create_bench_infra.sql` | `stats`, `processed` tables |
| `20260329000005_create_coordinated_tasks.sql` | `coordinated_tasks` table |

## Migration Tool Entities

### `cmd/migrate/main.go` (new)

A Go CLI binary that applies the goose migration set against a YDB Dedicated endpoint. Not a schema entity itself — it is the tool that creates and manages schema entities.

**Inputs (env vars)**:
- `YDB_ENDPOINT` — full gRPC endpoint string (e.g. `grpcs://ydb.serverless.yandexcloud.net:2135/...`)
- `YDB_SA_KEY_FILE` — (optional) path to SA key JSON file; if absent, metadata-service credentials are used

**Behaviour**: Calls `goose.Provider.Up()` using `goose.DialectYdB` with `ScriptingQueryMode` + `FakeTx` + `AutoDeclare` + `NumericArgs` — the same configuration used in `testhelper/ydb.go`.
