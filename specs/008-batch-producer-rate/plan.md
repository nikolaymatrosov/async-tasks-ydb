# Implementation Plan: Batch Producer Rate Control

**Branch**: `008-batch-producer-rate` | **Date**: 2026-04-22 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/008-batch-producer-rate/spec.md`

## Summary

Rework the `04_coordinated_table/producer.go` insertion loop from per-task ticker-driven writes into batch-per-window writes. The producer computes a batch size of `rate * window` tasks, assembles rows for each fixed time window, and submits them as a single YDB multi-row `UPSERT` using `AS_TABLE($records)` with a list of structs. Windows are serialized (no overlap), so slow storage caps throughput at the target rate without unbounded memory growth and without post-recovery bursts. Throughput is logged every 5 seconds against observed inserts, not the configured target, and producer-side Prometheus metrics are exposed on the existing `/metrics` endpoint so an operator can visualise whether the system keeps up with the configured target.

## Technical Context

**Language/Version**: Go 1.26 (as declared in `go.mod`)
**Primary Dependencies**: `ydb-go-sdk/v3 v3.127.0` (`query`, `types`, `ParamsBuilder`), `murmur3 v1.1.8`, `uuid v1.6.0` — all already in `go.mod`; no new direct dependencies
**Storage**: YDB — existing `coordinated_tasks` table; no schema changes. Batch `UPSERT` via `AS_TABLE($records)` where `$records` is a `List<Struct<...>>`
**Testing**: Manual end-to-end via `go run ./04_coordinated_table/ --mode producer` against live YDB; inserted row count verified with a `SELECT COUNT(*)` over a measured wall-clock window
**Target Platform**: Linux server / macOS (local dev)
**Project Type**: CLI example (self-contained runnable demo) — only `producer.go` changes
**Performance Goals**: Observed insertion rate within ±5% of configured target for rates from 1 to 10,000 tasks/sec, measured after a 30-second warm-up (SC-001)
**Constraints**: Pending-task memory bounded to one batch window's worth of rows (SC-002); no overlapping batch submissions; no burst on storage-recovery (SC-004); preserve existing task fields and the ~10% `scheduled_at` behavior (FR-007, FR-008)
**Observability**: Prometheus metrics on the existing `/metrics` HTTP endpoint (already wired in `main.go`). Producer mode exposes `producer_inserted_total`, `producer_batches_total`, `producer_batch_errors_total`, `producer_batch_size` (histogram), `producer_batch_duration_seconds` (histogram), `producer_observed_rate` (gauge), `producer_target_rate` (gauge), `producer_window_seconds` (gauge), `producer_backpressure_total` (counter incremented when a batch's UPSERT takes ≥ `window`), `producer_up` (gauge). These let a Grafana dashboard show observed-vs-target rate and surface backpressure.
**Scale/Scope**: 1 – 10,000 tasks/sec configured rate, default 100ms batch window (batch sizes ~1–1,000 rows), single producer process

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Verify compliance with each principle in `.specify/memory/constitution.md v1.0.0`:

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ✅ (existing 04_coordinated_table multi-file deviation inherited from feature 004; no new files added) |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ (existing `signal.NotifyContext` in `main.go` unchanged; batch loop honours `ctx.Done()` between windows) |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ (no schema changes; existing `coordinated_tasks` table covers batch UPSERT) |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ (unchanged; new `--batch-window` and `--report-interval` exposed as CLI flags with defaults) |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ (periodic throughput log uses `slog.Info` with `inserted`, `rate_observed`, `window_ms` fields) |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ (no new direct deps; murmur3 still used for partition hash per row) |

No ❌ entries — Complexity Tracking not required.

## Project Structure

### Documentation (this feature)

```text
specs/008-batch-producer-rate/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command — NOT created here)
```

No `contracts/` directory: the feature has no externally exposed interface — CLI flags are the only user-facing contract and are captured in `quickstart.md`.

### Source Code (repository root)

```text
04_coordinated_table/
├── main.go              # EDIT: add --batch-window + --report-interval flags; dispatch
│                        #       ProducerStats (producer mode) vs Stats (worker mode) to /metrics
├── producer.go          # REWRITE: batch-per-window loop, multi-row UPSERT via AS_TABLE($records),
│                        #          record producer_* metrics, periodic slog throughput report
├── producer_stats.go    # NEW: ProducerStats struct + registry with producer_* Prometheus metrics
├── worker.go            # unchanged
├── rebalancer.go        # unchanged
├── display.go           # unchanged (worker-only Stats)
├── metrics.go           # EDIT: generalise metricsHandler(registry *prometheus.Registry)
│                        #       so both ProducerStats.registry and Stats.registry can plug in
└── utils.go             # unchanged
```

**Structure Decision**: In-place modification of the existing `04_coordinated_table` example. One new file (`producer_stats.go`) holds producer-mode Prometheus wiring, mirroring how `display.go` holds worker-mode `Stats`. Only `producer.go` (algorithm rewrite), `main.go` (two new flags + mode-aware metrics wiring), and `metrics.go` (handler accepts a registry rather than a `*Stats`) are touched on the existing side. The multi-file layout of this example is pre-approved under feature 004's Complexity Tracking entry — this feature inherits that justification and does not extend it.

## Complexity Tracking

> No violations to justify. Section intentionally empty.
