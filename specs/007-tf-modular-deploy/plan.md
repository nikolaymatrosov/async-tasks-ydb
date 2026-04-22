# Implementation Plan: Terraform Modular Deployment

**Branch**: `007-tf-modular-deploy` | **Date**: 2026-04-22 | **Spec**: [spec.md](spec.md)  
**Input**: Feature specification from `/specs/007-tf-modular-deploy/spec.md`

## Summary

Reorganise the flat `terraform/` directory into three independently deployable child modules (`db`, `workers`, `producer`) composed by a root module. Add a new `producer` module that deploys the `01_db_producer` container as a Yandex Compute instance group. All existing variables, outputs, and `terraform.tfvars` files remain backward-compatible.

## Technical Context

**Language/Version**: HCL (Terraform ≥ 1.5)  
**Primary Dependencies**: `yandex-cloud/yandex` provider, `hashicorp/null ≥ 3.0`, `hashicorp/external ≥ 2.0`, `think-it-labs/dirhash 0.0.1` — all already in `.terraform.lock.hcl`; no new provider additions  
**Storage**: YDB Dedicated — existing `coordinated_tasks` table; no schema changes  
**Testing**: Manual `terraform plan -target=module.<name>` runs against a live cloud folder (no automated Terraform test suite)  
**Target Platform**: Yandex Cloud (compute instance groups, YDB, container registry, VPC, IAM)  
**Project Type**: Infrastructure-as-Code (Terraform child modules + root composer)  
**Performance Goals**: Full apply < 15 minutes on a fresh cloud folder (SC-004)  
**Constraints**: Zero breaking changes to existing `terraform.tfvars` files (SC-006); existing outputs must survive (SC-005)  
**Scale/Scope**: 3 child modules, ~10 Terraform resource types, ~20 variables total

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Verify compliance with each principle in `.specify/memory/constitution.md v1.0.0`:

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ❌ N/A — this feature adds no Go example code |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ❌ N/A — infrastructure-only; no Go runtime code |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ No new schema changes; existing migrations untouched |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ All credentials/endpoints remain in `terraform.tfvars` or passed as variables |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ❌ N/A — Terraform HCL only |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ❌ N/A — no Go code introduced |

Any ❌ MUST be justified in the Complexity Tracking table below.

## Project Structure

### Documentation (this feature)

```text
specs/007-tf-modular-deploy/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (Terraform module interface contracts)
└── tasks.md             # Phase 2 output (/speckit-tasks command)
```

### Source Code (repository root)

```text
terraform/
├── main.tf                     # Root: terraform block + providers + module calls
├── variables.tf                # Root: all input variables (unchanged public interface)
├── outputs.tf                  # Root: aggregate outputs from child modules
├── terraform.tfvars            # Unchanged
├── terraform.tfvars.example    # Updated to document new producer_* variables
├── .terraform.lock.hcl         # Unchanged
├── .terraformignore            # Unchanged
├── modules/
│   ├── db/
│   │   ├── variables.tf        # cloud_id, folder_id, zone, subnet_cidrs, ydb_*, registry_name
│   │   ├── outputs.tf          # ydb_endpoint, ydb_database_path, registry_id, sa_id, subnet_ids, network_id, registry_url
│   │   ├── network.tf          # yandex_vpc_network + yandex_vpc_subnet
│   │   ├── ydb.tf              # yandex_ydb_database_dedicated + dirhash data source
│   │   ├── registry.tf         # yandex_container_registry
│   │   └── iam.tf              # yandex_iam_service_account + all folder IAM bindings
│   ├── workers/
│   │   ├── variables.tf        # folder_id, zone, platform_id, vm_*, ig_*, worker_rate + db outputs (registry_url, registry_id, sa_id, ydb_endpoint, ydb_database, subnet_ids)
│   │   ├── outputs.tf          # instance_group_id, coordinator_image, cdc_worker_image, topic_bench_image, migrations_image, migrations_run_cmd
│   │   ├── containers.tf       # git_hash data, locals for image tags, null_resource builds for coordinator/cdc_worker/topic_bench/migrations
│   │   ├── compute.tf          # COI image data + yandex_compute_instance_group.workers
│   │   ├── docker-compose.yml.tpl
│   │   └── ua-config.yml.tpl
│   └── producer/
│       ├── variables.tf        # folder_id, zone, platform_id, vm_*, producer_rate + db outputs (registry_url, registry_id, sa_id, ydb_endpoint, ydb_database, subnet_ids)
│       ├── outputs.tf          # producer_instance_group_id, db_producer_image
│       ├── containers.tf       # git_hash data, local for db_producer_image, null_resource db_producer build
│       ├── compute.tf          # COI image data + yandex_compute_instance_group.producer
│       └── docker-compose.yml.tpl  # runs db-producer container
```

**Structure Decision**: Three child modules under `terraform/modules/` composed by the root `terraform/` module. The root module preserves the existing flat variable and output namespace. The `.terraform/` providers directory and state files remain at the root.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
| --------- | ---------- | ----------------------------------- |
| I — no single `main.go` | Feature is purely Terraform HCL; no Go example code is introduced | Creating a Go example is out of scope and unrelated to the infrastructure reorganisation goal |
| II — no lifecycle code | Terraform has no runtime lifecycle; the Go examples it deploys (01_db_producer, 04_coordinated_table) already implement lifecycle correctly | N/A |
| V — no slog | Terraform HCL has no application logging | N/A |
| Tech Constraints | No Go code added; all Terraform HCL | Terraform is an existing part of the project stack |
