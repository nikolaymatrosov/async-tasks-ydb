# Implementation Plan: Restructure 04 Example into pkg/cmd Layout

**Branch**: `009-04-restructure-pkg-cmd` | **Date**: 2026-04-23 | **Spec**: [spec.md](spec.md)  
**Input**: Feature specification from `specs/009-04-restructure-pkg-cmd/spec.md`

## Summary

Reorganise `04_coordinated_table/` from a flat `package main` into a standard Go `cmd/` + `pkg/` multi-binary layout, splitting producer and worker into independent binaries while extracting all shared logic into six purpose-named packages under `pkg/`. No runtime behaviour changes; the `--mode` flag is removed in favour of binary identity.

## Technical Context

**Language/Version**: Go 1.26 (as declared in `go.mod`)  
**Primary Dependencies**: `ydb-go-sdk/v3 v3.127.0`, `ydb-go-yc v0.12.3`, `murmur3 v1.1.8`, `uuid v1.6.0`, `prometheus/client_golang`, `golang.org/x/sync` — all already in `go.mod`; no new direct dependencies  
**Storage**: YDB — existing `coordinated_tasks` table; no schema changes  
**Testing**: Manual end-to-end against live YDB instance (no automated test suite per constitution)  
**Target Platform**: Linux server (same as current example)  
**Project Type**: Multi-binary CLI refactor within a single Go module  
**Performance Goals**: Identical to current — no performance changes  
**Constraints**: No new `go.mod` direct dependencies; module root stays at repo root  
**Scale/Scope**: Single example directory restructure — ~9 source files reorganised into ~10 files across 8 directories

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Verify compliance with each principle in `.specify/memory/constitution.md v1.0.0`:

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ❌ (justified — see Complexity Tracking) |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ (no schema changes) |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ |

Any ❌ MUST be justified in the Complexity Tracking table below.

## Project Structure

### Documentation (this feature)

```text
specs/009-04-restructure-pkg-cmd/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output — package boundary map
├── quickstart.md        # Phase 1 output — build + run instructions
├── contracts/
│   ├── producer-cli.md  # Phase 1 output — producer flag contract
│   └── worker-cli.md    # Phase 1 output — worker flag contract
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
04_coordinated_table/
├── cmd/
│   ├── producer/
│   │   └── main.go        ← producer entry point (flags, ydbconn.Open, metrics, taskproducer.Produce)
│   └── worker/
│       └── main.go        ← worker entry point (flags, ydbconn.Open, CreateNode, metrics, Worker.run)
├── pkg/
│   ├── uid/
│   │   └── uid.go         ← generateUUID() (error-returning primitive)
│   ├── metrics/
│   │   ├── handler.go     ← metricsHandler(registry) http.Handler
│   │   ├── worker_stats.go ← Stats, newStats, readCounter, readGauge, display
│   │   └── producer_stats.go ← ProducerStats, newProducerStats
│   ├── rebalancer/
│   │   └── rebalancer.go  ← Rebalancer, partitionEvent, ceilDiv (unchanged logic)
│   ├── taskworker/
│   │   └── worker.go      ← Worker, lockedTask, minDuration (unchanged logic)
│   ├── taskproducer/
│   │   └── producer.go    ← taskRow, buildBatch, upsertBatch, Produce (unchanged logic)
│   └── ydbconn/
│       └── conn.go        ← Open(ctx, endpoint, database) (*ydb.Driver, error) + cred resolution
└── README.md              ← updated to reflect new layout and build commands
```

**Structure Decision**: `cmd/` + `pkg/` within the existing example top-level directory. The example root is no longer a Go package itself — `go run ./04_coordinated_table/` is replaced by `go run ./04_coordinated_table/cmd/producer/` and `go run ./04_coordinated_table/cmd/worker/`.

## Complexity Tracking

| Violation               | Why Needed                                   | Alternative Rejected              |
|-------------------------|----------------------------------------------|-----------------------------------|
| Principle I (sub-pkgs)  | Two binaries need separate entry points.     | `--mode` removed; split IS goal.  |
