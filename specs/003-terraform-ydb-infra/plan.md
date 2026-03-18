# Implementation Plan: Terraform Infrastructure for YDB Cluster and Container-Optimized VMs

**Branch**: `003-terraform-ydb-infra` | **Date**: 2026-03-17 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/003-terraform-ydb-infra/spec.md`

## Summary

Provision a complete Yandex Cloud infrastructure — managed YDB Serverless database, private container registry, and a container-optimized VM — using Terraform. All three repository examples (`01_db_producer`, `02_cdc_worker`, `03_topic`) are packaged as distroless container images, pushed to the registry, and auto-started on the COI VM via Docker Compose metadata. The VM's service account authenticates to both the registry and YDB, enabling end-to-end example execution from a single `terraform apply`.

## Technical Context

**Language/Version**: HCL (Terraform ≥ 1.5), Go 1.26 (existing examples, unchanged), Dockerfile (multi-stage builds)
**Primary Dependencies**: Terraform provider `yandex-cloud/yandex`, `gcr.io/distroless/static-debian12:nonroot` (container base image)
**Storage**: YDB Serverless (managed, provisioned by Terraform)
**Testing**: Manual — `terraform plan` for dry-run, `terraform apply` for provisioning, SSH to VM to verify containers (per constitution: no automated test suite)
**Target Platform**: Yandex Cloud (ru-central1 region)
**Project Type**: Infrastructure-as-Code (IaC) — Terraform configuration + Dockerfiles
**Performance Goals**: N/A — infrastructure provisioning, not runtime performance
**Constraints**: Single Yandex Cloud folder, local Terraform state, single VM (no auto-scaling)
**Scale/Scope**: 1 YDB database, 1 container registry, 3 container images, 1 COI VM, 1 VPC network, 1 service account

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Verify compliance with each principle in `.specify/memory/constitution.md v1.0.0`:

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ⚠️ N/A — this feature adds infrastructure (Terraform/Docker), not a Go example. Existing examples are unchanged. |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ⚠️ N/A — no new Go runtime code. |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ⚠️ N/A — Terraform manages cloud resources, not YDB schema. Existing migrations remain the source of truth for schema. |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ — Terraform uses `variables.tf` for all configurable inputs. Docker Compose passes `YDB_ENDPOINT` and `YDB_SA_KEY_FILE` to containers via env vars. No hardcoded credentials. |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ⚠️ N/A — no new Go runtime code. |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ❌ — Introduces HCL (Terraform) and Dockerfile languages. See Complexity Tracking. |

Any ❌ MUST be justified in the Complexity Tracking table below.

## Project Structure

### Documentation (this feature)

```text
specs/003-terraform-ydb-infra/
├── plan.md              # This file
├── research.md          # Phase 0 output — technology decisions
├── data-model.md        # Phase 1 output — resource model
├── quickstart.md        # Phase 1 output — deployment guide
├── contracts/           # Phase 1 output — Terraform interface contracts
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
terraform/
├── main.tf                    # Provider config, required_providers
├── variables.tf               # Input variables (cloud_id, folder_id, zone, sa_key_file)
├── outputs.tf                 # Output values (VM IP, YDB endpoint, registry ID)
├── ydb.tf                     # yandex_ydb_database_serverless resource
├── registry.tf                # yandex_container_registry resource
├── network.tf                 # yandex_vpc_network + yandex_vpc_subnet
├── iam.tf                     # yandex_iam_service_account + role bindings
├── compute.tf                 # yandex_compute_instance (COI VM)
├── docker-compose.yaml        # Docker Compose spec for COI metadata
└── terraform.tfvars.example   # Example variable values for users

Dockerfile                     # Multi-stage, parameterized with --build-arg EXAMPLE
```

**Structure Decision**: All Terraform files live in a dedicated `terraform/` directory at the repo root, cleanly separated from Go examples. A single parameterized `Dockerfile` at the repo root builds any example via `--build-arg EXAMPLE=01_db_producer`. The Docker Compose file lives inside `terraform/` since it's consumed by the COI VM metadata.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Tech Constraints: introduces HCL (Terraform) and Dockerfile — not Go | Infrastructure provisioning cannot be expressed in Go. Terraform is the industry-standard IaC tool for Yandex Cloud. Dockerfiles are required by FR-002 (distroless container images). | Manual CLI scripts (`yc` commands) are not declarative/idempotent (violates FR-008). Shell scripts are harder to maintain and don't track state. |
| Principles I–III, V: N/A for this feature | This feature creates infrastructure configuration, not a Go example application. The constitution principles target example source code, which this feature does not modify. | No alternative — the principles are structurally inapplicable to IaC. |
