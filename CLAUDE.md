# async-tasks-ydb Development Guidelines

Auto-generated from all feature plans. Last updated: 2026-04-25

## Active Technologies

- Go 1.26 (go.mod); HCL (Terraform ≥ 1.5) + `ydb-go-sdk/v3 v3.135.0`, `ydb-go-yc v0.12.3`, stdlib `net/http` (no new direct deps); Terraform provider `yandex-cloud/yandex` (005-04-autoscale-deploy)
- YDB (existing `coordinated_tasks` table — no schema changes) (005-04-autoscale-deploy)
- HCL (Terraform ≥ 1.5) + `yandex-cloud/yandex` provider, `hashicorp/null ≥ 3.0`, `hashicorp/external ≥ 2.0`, `think-it-labs/dirhash 0.0.1` — all already in `.terraform.lock.hcl`; no new provider additions (007-tf-modular-deploy)
- YDB Dedicated — existing `coordinated_tasks` table; no schema changes (007-tf-modular-deploy)
- Go 1.26 (as declared in `go.mod`) + `ydb-go-sdk/v3 v3.135.0` (`query`, `types`, `ParamsBuilder`), `murmur3 v1.1.8`, `uuid v1.6.0` — all already in `go.mod`; no new direct dependencies (008-batch-producer-rate)
- YDB — existing `coordinated_tasks` table; no schema changes. Batch `UPSERT` via `AS_TABLE($records)` where `$records` is a `List<Struct<...>>` (008-batch-producer-rate)
- Go 1.26 (as declared in `go.mod`) + `github.com/ydb-platform/ydb-go-sdk/v3 v3.135.0` (`query`, `query.Session`, `query.TxActor`, `ParamsBuilder`, `TxSettings`); stdlib `context`, `time`, `log/slog`. **No new direct `go.mod` dependencies.** (014-worker-repository-refactor)
- Existing `coordinated_tasks` table in YDB Serverless — schema, status vocabulary, and partition-key shape unchanged (FR-012). (014-worker-repository-refactor)

- Go 1.26 (go.mod) + `ydb-go-sdk/v3 v3.135.0`, `ydb-go-yc v0.12.3`, `murmur3 v1.1.8`, `uuid v1.6.0` — all already in go.mod; no new direct dependencies (002-topic-partition-bench)
- YDB — 2 topics (`tasks/by_user`, `tasks/by_message_id`, 10 partitions each) + 2 tables (`stats` for read-modify-write, `processed` for insert-only) (002-topic-partition-bench)
- HCL (Terraform ≥ 1.5), Go 1.26 (existing examples, unchanged), Dockerfile (multi-stage builds) + Terraform provider `yandex-cloud/yandex`, `gcr.io/distroless/static-debian12:nonroot` (container base image) (003-terraform-ydb-infra)
- YDB Serverless (managed, provisioned by Terraform) (003-terraform-ydb-infra)
- Go 1.26 (as declared in go.mod) + `ydb-go-sdk/v3 v3.135.0` (coordination + table APIs), `ydb-go-yc v0.12.3` (auth), `murmur3 v1.1.8` (hash routing), `uuid v1.6.0` (task IDs, lock values), `alitto/pond/v2` (worker pool — already in go.mod) (004-coordinated-table-workers)
- YDB Serverless — `coordinated_tasks` table + coordination node with 256 partition semaphores (004-coordinated-table-workers)

- Go 1.26 (as declared in go.mod) + `github.com/ydb-platform/ydb-go-sdk/v3 v3.135.0`, `github.com/ydb-platform/ydb-go-yc v0.12.3` (001-03-topic-writer)
- Go 1.26 (as declared in `go.mod`) + `ydb-go-sdk/v3 v3.135.0`, `ydb-go-yc v0.12.3`, `murmur3 v1.1.8`, `uuid v1.6.0`, `prometheus/client_golang`, stdlib `net/http`, `log/slog`, `sync/atomic` — all already in `go.mod`; no new direct dependencies (017-entity-task-ordering)
- YDB — new `ordered_tasks` table (PK `(partition_id, id)`, covering global index `idx_partition_entity_seq`); two new self-contained examples `05_ordered_tasks/` and `06_target_server/` (017-entity-task-ordering)

## Project Structure

```text
src/
tests/
```

## Commands

# Add commands for Go 1.26 (as declared in go.mod)

## Code Style

Go 1.26 (as declared in go.mod): Follow standard conventions

## Recent Changes

- 017-entity-task-ordering: Added per-entity ordered task delivery as `05_ordered_tasks/` (forked from `04_coordinated_table/`, drops `priority`/`hash`, partitions by `entity_id`, single-instance producer, head-of-entity dispatch) plus a new test target `06_target_server/`. New table `ordered_tasks` with covering index `idx_partition_entity_seq`. No new direct `go.mod` deps.
- 014-worker-repository-refactor: Added Go 1.26 (as declared in `go.mod`) + `github.com/ydb-platform/ydb-go-sdk/v3 v3.135.0` (`query`, `query.Session`, `query.TxActor`, `ParamsBuilder`, `TxSettings`); stdlib `context`, `time`, `log/slog`. **No new direct `go.mod` dependencies.**
- 008-batch-producer-rate: Added Go 1.26 (as declared in `go.mod`) + `ydb-go-sdk/v3 v3.135.0` (`query`, `types`, `ParamsBuilder`), `murmur3 v1.1.8`, `uuid v1.6.0` — all already in `go.mod`; no new direct dependencies
- 007-tf-modular-deploy: Added HCL (Terraform ≥ 1.5) + `yandex-cloud/yandex` provider, `hashicorp/null ≥ 3.0`, `hashicorp/external ≥ 2.0`, `think-it-labs/dirhash 0.0.1` — all already in `.terraform.lock.hcl`; no new provider additions

<!-- MANUAL ADDITIONS START -->
<!-- MANUAL ADDITIONS END -->

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at `specs/017-entity-task-ordering/plan.md`.
<!-- SPECKIT END -->
