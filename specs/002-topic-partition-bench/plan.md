# Implementation Plan: Topic Partition Benchmark

**Branch**: `002-topic-partition-bench` | **Date**: 2026-03-16 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/002-topic-partition-bench/spec.md`

## Summary

Rewrite `03_topic/` into a self-contained benchmark that publishes 100,000 messages to two YDB
topics (partitioned by user ID vs message ID), then runs 4 sequential consumer scenarios measuring
Transaction Lock Invalidation (TLI) errors for read-modify-write vs insert-only workloads. Prints
a human-readable comparison table. All new schema is in a goose migration; no new `go.mod`
direct dependencies are required.

## Technical Context

**Language/Version**: Go 1.26 (go.mod)
**Primary Dependencies**: `ydb-go-sdk/v3 v3.127.0`, `ydb-go-yc v0.12.3`, `murmur3 v1.1.8`, `uuid v1.6.0` — all already in go.mod; no new direct dependencies
**Storage**: YDB — 2 topics (`tasks/by_user`, `tasks/by_message_id`, 10 partitions each) + 2 tables (`stats` for read-modify-write, `processed` for insert-only)
**Testing**: Manual end-to-end validation against live YDB instance (`go run ./03_topic/`)
**Target Platform**: Linux server (cloud YDB / Yandex Cloud)
**Project Type**: CLI benchmark example (single binary)
**Performance Goals**: Demonstrate ≥10× TLI reduction for user-aligned partitioning vs random partitioning
**Constraints**: Complete 100,000-message benchmark per scenario in reasonable wall-clock time; zero memory leaks; clean shutdown on SIGTERM/SIGINT
**Scale/Scope**: 10 partitions per topic, 100 distinct users, 100,000 messages default

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./03_topic/` | ✅ |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ |

No violations. Complexity Tracking not required.

## Project Structure

### Documentation (this feature)

```text
specs/002-topic-partition-bench/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── cli-flags.md
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

```text
03_topic/
├── main.go              # REWRITE: benchmark orchestrator (flags, connect, produce, 4 scenarios, table)
├── producer.go          # CREATE: extract Producer, safeWriter, hashKey; add BenchMessage generation
├── consumer.go          # CREATE: Consumer struct, per-partition readers, TLI tracking
├── message.go           # CREATE: BenchMessage struct + JSON marshal/unmarshal
├── utils.go             # KEEP AS-IS: UserIDSampler
└── utils_test.go        # KEEP AS-IS: sampler tests

migrations/
└── 20260316000004_create_bench_infra.sql  # CREATE: topics + tables DDL
```

**Structure Decision**: Single example directory (`03_topic/`) following Principle I. Multiple `.go`
files within the same `package main` are permitted because the package is a single compilation
unit; `go run ./03_topic/` compiles all files together. Each file has a single clear responsibility
(orchestration / production / consumption / message types / utilities), keeping `main.go` focused
on the benchmark flow without sub-packages.
