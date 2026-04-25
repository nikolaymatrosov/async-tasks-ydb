# Implementation Plan: Per-IG Service Account Isolation with Safe Dependency Ordering

**Branch**: `015-tf-ig-sa-refactor` | **Date**: 2026-04-25 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/015-tf-ig-sa-refactor/spec.md`

## Summary

Refactor the Yandex Cloud Terraform configuration so each Instance Group (producer, workers) owns two dedicated Service Accounts — one for IG management and one for VM identity — co-located in the respective module. IAM role bindings are expressed as `depends_on` targets of the `yandex_compute_instance_group` resource, ensuring Terraform always destroys the IG before removing any of its IAM bindings. The shared `coi_vm` SA in the `db` module is removed.

## Technical Context

**Language/Version**: HCL (Terraform ≥ 1.5, as already in use)
**Primary Dependencies**: Yandex Cloud provider `yandex-cloud/yandex` (already in `.terraform.lock.hcl`); no new providers
**Storage**: N/A — no schema changes; YDB `coordinated_tasks` table untouched
**Testing**: Manual `terraform plan` + `terraform apply` against a live Yandex Cloud folder; destroy plan order inspection
**Target Platform**: Yandex Cloud (cloud/folder already provisioned)
**Project Type**: Infrastructure-as-code (Terraform modules)
**Performance Goals**: N/A — no runtime performance impact; apply time expected unchanged
**Constraints**: SA names ≤ 63 chars, lowercase alphanumeric + hyphens; no new Terraform providers
**Scale/Scope**: 3 modules modified (`producer`, `workers`, `db`), 1 root file modified (`main.tf`)

## Constitution Check

The constitution principles govern Go example code, not Terraform infrastructure files. All five principles are N/A for this feature.

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples | N/A — Terraform refactor, no Go example involved |
| II. Lifecycle Completeness | N/A — no Go goroutines or signal handling |
| III. Schema-Managed Persistence | N/A — no YDB schema changes |
| IV. Environment-Variable Configuration | N/A — Terraform uses tfvars, not env vars for infra config |
| V. Structured Logging | N/A — no Go code |
| Tech Constraints | N/A — no new Go dependencies |

## Project Structure

### Documentation (this feature)

```text
specs/015-tf-ig-sa-refactor/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
└── tasks.md             # Phase 2 output (created by /speckit-tasks)
```

### Source Code (files modified by this feature)

```text
terraform/
├── main.tf                          # Remove service_account_id from producer + workers module calls
└── modules/
    ├── producer/
    │   ├── iam.tf                   # NEW: producer_ig SA, producer_vm SA, all 7 IAM bindings
    │   ├── compute.tf               # MODIFY: service_account_id references, add depends_on
    │   └── variables.tf             # MODIFY: remove service_account_id variable
    ├── workers/
    │   ├── iam.tf                   # NEW: workers_ig SA, workers_vm SA, all 7 IAM bindings
    │   ├── compute.tf               # MODIFY: service_account_id references, add depends_on
    │   └── variables.tf             # MODIFY: remove service_account_id variable
    └── db/
        ├── iam.tf                   # MODIFY: remove coi_vm SA + its 7 bindings; keep bastion SA
        └── outputs.tf               # MODIFY: remove service_account_id output
```

## Implementation Steps

### Step 1: Create `terraform/modules/producer/iam.tf`

Create a new file with:

- `yandex_iam_service_account.producer_ig` named `async-tasks-producer-ig-sa`
- `yandex_iam_service_account.producer_vm` named `async-tasks-producer-vm-sa`
- 4 IAM bindings for `producer_ig`: `compute.editor`, `iam.serviceAccounts.user`, `vpc.user`, `vpc.publicAdmin`
- 3 IAM bindings for `producer_vm`: `container-registry.images.puller`, `ydb.editor`, `monitoring.editor`

### Step 2: Modify `terraform/modules/producer/compute.tf`

- Change `service_account_id = var.service_account_id` (IG level) → `yandex_iam_service_account.producer_ig.id`
- Change `service_account_id = var.service_account_id` (instance_template level) → `yandex_iam_service_account.producer_vm.id`
- Add `depends_on` to `yandex_compute_instance_group.producer` listing all 7 IAM binding resources (keeping existing `null_resource.coordinator_image` dependency)

### Step 3: Modify `terraform/modules/producer/variables.tf`

- Remove the `service_account_id` variable block entirely

### Step 4: Create `terraform/modules/workers/iam.tf`

Create a new file with:

- `yandex_iam_service_account.workers_ig` named `async-tasks-workers-ig-sa`
- `yandex_iam_service_account.workers_vm` named `async-tasks-workers-vm-sa`
- 4 IAM bindings for `workers_ig`: `compute.editor`, `iam.serviceAccounts.user`, `vpc.user`, `vpc.publicAdmin`
- 3 IAM bindings for `workers_vm`: `container-registry.images.puller`, `ydb.editor`, `monitoring.editor`

### Step 5: Modify `terraform/modules/workers/compute.tf`

- Change `service_account_id = var.service_account_id` (IG level) → `yandex_iam_service_account.workers_ig.id`
- Change `service_account_id = var.service_account_id` (instance_template level) → `yandex_iam_service_account.workers_vm.id`
- Add `depends_on` to `yandex_compute_instance_group.workers` listing all 7 IAM binding resources

### Step 6: Modify `terraform/modules/workers/variables.tf`

- Remove the `service_account_id` variable block entirely

### Step 7: Modify `terraform/modules/db/iam.tf`

- Remove `yandex_iam_service_account.coi_vm` and all 7 of its IAM bindings
- Retain `yandex_iam_service_account.bastion` and its 2 bindings unchanged

### Step 8: Modify `terraform/modules/db/outputs.tf`

- Remove the `service_account_id` output block

### Step 9: Modify `terraform/main.tf`

- Remove `service_account_id = module.db.service_account_id` from the `module "workers"` block
- Remove `service_account_id = module.db.service_account_id` from the `module "producer"` block

### Step 10: Validate

- Run `terraform validate` in `terraform/` — must pass with no errors
- Run `terraform plan` — review that it plans to:
  - CREATE 4 new SAs (2 per IG module)
  - CREATE 14 new IAM bindings (7 per IG module)
  - DESTROY 1 old SA (`coi_vm`)
  - DESTROY 7 old IAM bindings (from db module)
  - UPDATE 2 `yandex_compute_instance_group` resources (SA references + depends_on)
- Inspect destroy plan: IGs must be destroyed before their IAM bindings
- Run `terraform apply` against live environment
- Verify VMs start successfully (image pull, YDB connection, metrics)

## Complexity Tracking

> No constitution violations — this feature involves no Go code.
