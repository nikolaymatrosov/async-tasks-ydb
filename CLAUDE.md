# async-tasks-ydb Development Guidelines

Auto-generated from all feature plans. Last updated: 2026-03-17

## Active Technologies
- Go 1.26 (go.mod) + `ydb-go-sdk/v3 v3.127.0`, `ydb-go-yc v0.12.3`, `murmur3 v1.1.8`, `uuid v1.6.0` — all already in go.mod; no new direct dependencies (002-topic-partition-bench)
- YDB — 2 topics (`tasks/by_user`, `tasks/by_message_id`, 10 partitions each) + 2 tables (`stats` for read-modify-write, `processed` for insert-only) (002-topic-partition-bench)
- HCL (Terraform ≥ 1.5), Go 1.26 (existing examples, unchanged), Dockerfile (multi-stage builds) + Terraform provider `yandex-cloud/yandex`, `gcr.io/distroless/static-debian12:nonroot` (container base image) (003-terraform-ydb-infra)
- YDB Serverless (managed, provisioned by Terraform) (003-terraform-ydb-infra)

- Go 1.26 (as declared in go.mod) + `github.com/ydb-platform/ydb-go-sdk/v3 v3.127.0`, `github.com/ydb-platform/ydb-go-yc v0.12.3` (001-03-topic-writer)

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
- 003-terraform-ydb-infra: Added HCL (Terraform ≥ 1.5), Go 1.26 (existing examples, unchanged), Dockerfile (multi-stage builds) + Terraform provider `yandex-cloud/yandex`, `gcr.io/distroless/static-debian12:nonroot` (container base image)
- 002-topic-partition-bench: Added Go 1.26 (go.mod) + `ydb-go-sdk/v3 v3.127.0`, `ydb-go-yc v0.12.3`, `murmur3 v1.1.8`, `uuid v1.6.0` — all already in go.mod; no new direct dependencies

- 001-03-topic-writer: Added Go 1.26 (as declared in go.mod) + `github.com/ydb-platform/ydb-go-sdk/v3 v3.127.0`, `github.com/ydb-platform/ydb-go-yc v0.12.3`

<!-- MANUAL ADDITIONS START -->
<!-- MANUAL ADDITIONS END -->
