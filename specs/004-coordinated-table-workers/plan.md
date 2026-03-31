# Implementation Plan: Coordinated Table Workers

**Branch**: `004-coordinated-table-workers` | **Date**: 2026-03-29 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/004-coordinated-table-workers/spec.md`

## Summary

Add a new `04_coordinated_table` example demonstrating producer-consumer task processing coordinated via YDB coordination node semaphores. A producer inserts prioritized tasks (0-255) with optional scheduled_at into a YDB table. 8 consumer workers acquire exclusive ownership of 256 logical partitions (hash % 256) via coordination semaphores and process tasks in priority order, skipping postponed tasks. Workers use greedy acquisition with capacity limiting for decentralized partition rebalancing on membership changes.

## Technical Context

**Language/Version**: Go 1.26 (as declared in go.mod)
**Primary Dependencies**: `ydb-go-sdk/v3 v3.127.0` (coordination + table APIs), `ydb-go-yc v0.12.3` (auth), `murmur3 v1.1.8` (hash routing), `uuid v1.6.0` (task IDs, lock values), `alitto/pond/v2` (worker pool — already in go.mod)
**Storage**: YDB Serverless — `coordinated_tasks` table + coordination node with 256 partition semaphores
**Testing**: Manual end-to-end via `go run ./04_coordinated_table/` against live YDB; integration tests using testhelper (testcontainers YDB)
**Target Platform**: Linux server / macOS (local dev)
**Project Type**: CLI example (self-contained runnable demo)
**Performance Goals**: All 256 partitions assigned within 10s; partition reassignment within 5s of worker death
**Constraints**: Single `main.go` per constitution (with justified multi-file deviation following 03_topic precedent); no new direct dependencies
**Scale/Scope**: 256 logical partitions, 8 concurrent workers, continuous producer

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Verify compliance with each principle in `.specify/memory/constitution.md v1.0.0`:

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ❌ (justified) |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ |

Any ❌ MUST be justified in the Complexity Tracking table below.

## Project Structure

### Documentation (this feature)

```text
specs/004-coordinated-table-workers/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
04_coordinated_table/
├── main.go              # Entry point: flags, YDB connection, signal handling, mode dispatch
├── producer.go          # Task producer: inserts rows with hash, priority, scheduled_at
├── worker.go            # Consumer worker: partition ownership, task polling, locking, processing
├── rebalancer.go        # Partition rebalancing: semaphore acquisition, capacity management, watch
└── display.go           # Stats display: periodic summary of partitions owned, tasks processed

migrations/
└── 20260329000005_create_coordinated_tasks.sql  # coordinated_tasks table + coordination node DDL
```

**Structure Decision**: Multi-file layout within `04_coordinated_table/` directory, following the precedent set by `03_topic/` which already uses separate files (producer.go, consumer.go, message.go, display.go, utils.go). The feature involves four distinct concerns (producing, consuming, rebalancing, display) that would make a single main.go ~800+ lines and difficult to follow. All files remain in the same package (`package main`) so `go run ./04_coordinated_table/` works as required.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| I. Multi-file layout (not single main.go) | Feature has 4 distinct concerns: producer, worker, rebalancer, display. Estimated ~800 lines combined. | Single main.go would be unreadable for a learning example. 03_topic already established multi-file precedent with 6 source files. All files remain `package main` in one directory — `go run ./04_coordinated_table/` still works. |
