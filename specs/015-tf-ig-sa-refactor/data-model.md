# Data Model: Per-IG Service Account Isolation

This feature adds no new application data entities. It restructures Terraform IAM resources. The "data model" here describes the Terraform resource graph after the refactor.

## Resource Inventory: Producer Module

### `yandex_iam_service_account.producer_ig` (new)

| Attribute | Value |
|-----------|-------|
| name | `async-tasks-producer-ig-sa` |
| folder_id | `var.folder_id` |
| Purpose | IG control plane identity (manages VMs) |

### `yandex_iam_service_account.producer_vm` (new)

| Attribute | Value |
|-----------|-------|
| name | `async-tasks-producer-vm-sa` |
| folder_id | `var.folder_id` |
| Purpose | VM runtime identity (application processes) |

### IAM Bindings for `producer_ig` (new, 4 resources)

| Resource Name | Role | Member |
|---|---|---|
| `producer_ig_compute_editor` | `compute.editor` | `producer_ig` SA |
| `producer_ig_sa_user` | `iam.serviceAccounts.user` | `producer_ig` SA |
| `producer_ig_vpc_user` | `vpc.user` | `producer_ig` SA |
| `producer_ig_vpc_public_admin` | `vpc.publicAdmin` | `producer_ig` SA |

### IAM Bindings for `producer_vm` (new, 3 resources)

| Resource Name | Role | Member |
|---|---|---|
| `producer_vm_registry_puller` | `container-registry.images.puller` | `producer_vm` SA |
| `producer_vm_ydb_editor` | `ydb.editor` | `producer_vm` SA |
| `producer_vm_monitoring_editor` | `monitoring.editor` | `producer_vm` SA |

### `yandex_compute_instance_group.producer` (modified)

| Field | Before | After |
|---|---|---|
| `service_account_id` | `var.service_account_id` | `yandex_iam_service_account.producer_ig.id` |
| `instance_template.service_account_id` | `var.service_account_id` | `yandex_iam_service_account.producer_vm.id` |
| `depends_on` | `[null_resource.coordinator_image]` | `[null_resource.coordinator_image, all 7 IAM bindings above]` |

---

## Resource Inventory: Workers Module

### `yandex_iam_service_account.workers_ig` (new)

| Attribute | Value |
|-----------|-------|
| name | `async-tasks-workers-ig-sa` |
| folder_id | `var.folder_id` |
| Purpose | IG control plane identity (manages autoscale VMs) |

### `yandex_iam_service_account.workers_vm` (new)

| Attribute | Value |
|-----------|-------|
| name | `async-tasks-workers-vm-sa` |
| folder_id | `var.folder_id` |
| Purpose | VM runtime identity (worker processes) |

### IAM Bindings for `workers_ig` (new, 4 resources)

| Resource Name | Role | Member |
|---|---|---|
| `workers_ig_compute_editor` | `compute.editor` | `workers_ig` SA |
| `workers_ig_sa_user` | `iam.serviceAccounts.user` | `workers_ig` SA |
| `workers_ig_vpc_user` | `vpc.user` | `workers_ig` SA |
| `workers_ig_vpc_public_admin` | `vpc.publicAdmin` | `workers_ig` SA |

### IAM Bindings for `workers_vm` (new, 3 resources)

| Resource Name | Role | Member |
|---|---|---|
| `workers_vm_registry_puller` | `container-registry.images.puller` | `workers_vm` SA |
| `workers_vm_ydb_editor` | `ydb.editor` | `workers_vm` SA |
| `workers_vm_monitoring_editor` | `monitoring.editor` | `workers_vm` SA |

### `yandex_compute_instance_group.workers` (modified)

| Field | Before | After |
|---|---|---|
| `service_account_id` | `var.service_account_id` | `yandex_iam_service_account.workers_ig.id` |
| `instance_template.service_account_id` | `var.service_account_id` | `yandex_iam_service_account.workers_vm.id` |
| `depends_on` | (none) | all 7 IAM bindings above |

---

## Removed Resources: db Module

| Resource | Action |
|---|---|
| `yandex_iam_service_account.coi_vm` | Removed |
| `yandex_resourcemanager_folder_iam_member.registry_puller` | Removed |
| `yandex_resourcemanager_folder_iam_member.ydb_editor` | Removed |
| `yandex_resourcemanager_folder_iam_member.monitoring_editor` | Removed |
| `yandex_resourcemanager_folder_iam_member.vpc_user` | Removed |
| `yandex_resourcemanager_folder_iam_member.vpc_public_admin` | Removed |
| `yandex_resourcemanager_folder_iam_member.compute_editor` | Removed |
| `yandex_resourcemanager_folder_iam_member.iam_sa_user` | Removed |
| `output.service_account_id` (db module) | Removed |

## Variable Changes

| Module | Variable | Action |
|---|---|---|
| producer | `service_account_id` | Removed |
| workers | `service_account_id` | Removed |
| main.tf → producer | `service_account_id = module.db.service_account_id` | Removed |
| main.tf → workers | `service_account_id = module.db.service_account_id` | Removed |

## Dependency Graph (simplified)

```
producer_ig SA ──── (depends_on) ──────────────────────────┐
producer_ig IAM bindings ──────────────────────────────────► producer IG ─► destroy first on `terraform destroy`
producer_vm SA ──── (depends_on) ──────────────────────────┘
producer_vm IAM bindings ──────────────────────────────────┘

workers_ig SA ──── (depends_on) ────────────────────────────┐
workers_ig IAM bindings ────────────────────────────────────► workers IG ─► destroy first on `terraform destroy`
workers_vm SA ──── (depends_on) ────────────────────────────┘
workers_vm IAM bindings ────────────────────────────────────┘
```
