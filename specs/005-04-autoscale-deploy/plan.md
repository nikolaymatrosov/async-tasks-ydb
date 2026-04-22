# Implementation Plan: Load-Test & Autoscaling Deployment for Example 04

**Branch**: `005-04-autoscale-deploy` | **Date**: 2026-04-22 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/005-04-autoscale-deploy/spec.md`

## Summary

Deploy the `04_coordinated_table` example to a Yandex Cloud autoscaling instance group to validate 115 RPS / 10M events-per-day throughput. Each VM boots a Container-Optimized Image, reads a docker-compose definition from instance metadata, and runs two containers: the coordinator worker and a Unified Agent sidecar. The sidecar scrapes a new Prometheus `/metrics` endpoint added to the Go binary and forwards metrics — plus host-level CPU/memory — to Yandex Monitoring. CPU utilisation drives autoscaling.

## Technical Context

**Language/Version**: Go 1.26 (go.mod); HCL (Terraform ≥ 1.5)
**Primary Dependencies**: `ydb-go-sdk/v3 v3.127.0`, `ydb-go-yc v0.12.3`, stdlib `net/http` (no new direct deps); Terraform provider `yandex-cloud/yandex`
**Storage**: YDB (existing `coordinated_tasks` table — no schema changes)
**Testing**: Manual end-to-end via `go run ./04_coordinated_table/` + live YDB; `go vet ./04_coordinated_table/` as build gate
**Target Platform**: Yandex Cloud COI (Container-Optimized Image), Linux amd64
**Project Type**: Infrastructure deployment + Go binary enhancement
**Performance Goals**: ≥ 115 RPS sustained for 30 min, < 1% task loss
**Constraints**: No new Go direct dependencies; single docker-compose metadata key per VM; metrics scrape latency < 5ms
**Scale/Scope**: 1–5 VM instances (instance group); 256 YDB partitions; ~4 Prometheus metrics counters

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ✅ `04_coordinated_table/` is self-contained. Adding `metrics.go` follows existing pattern (multiple `.go` files, same `package main` — see pre-existing `worker.go`, `producer.go`, `display.go`). No new sub-packages. |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ Existing lifecycle is complete. The new metrics HTTP server will be stopped as part of the existing shutdown sequence. |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ No new YDB schema changes in this feature. |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ Existing env vars preserved. New `--metrics-port` flag follows the CLI-flag pattern. YDB endpoint and database passed via env/flags in docker-compose template — no hardcoding. |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ Metrics server startup logged via `slog.Info`. No changes to existing logging. |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ No changes to routing or migrations. No new direct deps. |

**Post-design re-check**: All principles remain satisfied after Phase 1 design. No violations to justify.

## Project Structure

### Documentation (this feature)

```text
specs/005-04-autoscale-deploy/
├── plan.md              # This file
├── research.md          # Phase 0 complete
├── data-model.md        # Phase 1 complete
├── quickstart.md        # Phase 1 complete
├── contracts/
│   ├── metrics-endpoint.md       # Phase 1 complete
│   └── terraform-variables.md   # Phase 1 complete
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code Changes

```text
04_coordinated_table/
├── main.go              (MODIFIED — start metrics server; add --metrics-port flag)
├── metrics.go           (NEW — Prometheus /metrics HTTP handler, reads Stats atomics)
├── display.go           (MODIFIED — add tasksErrors atomic counter to Stats)
├── worker.go            (MODIFIED — increment tasksErrors on lock/complete failure)
├── producer.go          (unchanged)
├── rebalancer.go        (unchanged)
└── utils.go             (unchanged)

terraform/
├── compute.tf           (MODIFIED — replace yandex_compute_instance with yandex_compute_instance_group)
├── iam.tf               (MODIFIED — add monitoring.editor role binding)
├── variables.tf         (MODIFIED — add ig_*, ydb_endpoint, ydb_database, worker_rate variables)
├── outputs.tf           (MODIFIED — add instance_group_id output)
├── docker-compose.yml.tpl  (NEW — template for COI metadata; app + UA sidecar)
├── ua-config.yml.tpl    (NEW — Unified Agent config template)
├── main.tf              (unchanged)
├── containers.tf        (unchanged)
├── network.tf           (unchanged)
├── ydb.tf               (unchanged)
└── registry.tf          (unchanged)
```

**Structure Decision**: Single-project layout. Go changes are additive to the existing `04_coordinated_table/` package. Terraform changes are in-place modifications to the existing `terraform/` module. Two new template files are added to `terraform/`.

## Complexity Tracking

> No Constitution Check violations.
