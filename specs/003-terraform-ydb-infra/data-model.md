# Data Model: Terraform Infrastructure for YDB Cluster and Container-Optimized VMs

**Feature Branch**: `003-terraform-ydb-infra` | **Date**: 2026-03-17

## Cloud Resource Entities

This feature provisions cloud infrastructure, not application-level data models. The "entities" are Yandex Cloud resources managed by Terraform.

### 1. YDB Database (Serverless)

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Database name (e.g., `async-tasks-ydb`) |
| `folder_id` | string | Yandex Cloud folder ID (from variable) |
| `location_id` | string | Regional location (`ru-central1`) |

**Terraform resource**: `yandex_ydb_database_serverless`
**Outputs**: `document_api_endpoint`, `ydb_full_endpoint` (gRPC connection string)

### 2. Container Registry

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Registry name (e.g., `async-tasks-registry`) |
| `folder_id` | string | Yandex Cloud folder ID |

**Terraform resource**: `yandex_container_registry`
**Outputs**: `id` (registry ID used in image paths: `cr.yandex/<id>/<image>:<tag>`)

### 3. Container Images (3 total)

| Image Name | Source Directory | Description |
|------------|-----------------|-------------|
| `01_db_producer` | `01_db_producer/` | Database producer example |
| `02_cdc_worker` | `02_cdc_worker/` | CDC worker example |
| `03_topic` | `03_topic/` | Topic partition benchmark example |

**Build process**: Multi-stage Dockerfile → `gcr.io/distroless/static-debian12:nonroot`
**Registry path**: `cr.yandex/<registry_id>/<image_name>:latest`

> Note: Container image build and push are **not** Terraform-managed. They happen via `docker build` + `docker push` as a prerequisite step before `terraform apply`. A helper script or Makefile target will orchestrate this.

### 4. Service Account

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | SA name (e.g., `coi-vm-sa`) |
| `folder_id` | string | Yandex Cloud folder ID |

**Terraform resource**: `yandex_iam_service_account`
**IAM roles** (via `yandex_resourcemanager_folder_iam_member`):

| Role | Purpose |
|------|---------|
| `container-registry.images.puller` | Pull images from registry on VM boot |
| `ydb.editor` | Read/write access to YDB database |

### 5. VPC Network

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Network name (e.g., `async-tasks-net`) |
| `folder_id` | string | Yandex Cloud folder ID |

**Terraform resource**: `yandex_vpc_network`

### 6. VPC Subnet

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Subnet name (e.g., `async-tasks-subnet`) |
| `zone` | string | Availability zone (e.g., `ru-central1-a`) |
| `network_id` | ref | References VPC Network |
| `v4_cidr_blocks` | list(string) | IPv4 CIDR (e.g., `["10.128.0.0/24"]`) |

**Terraform resource**: `yandex_vpc_subnet`

### 7. Compute Instance (COI VM)

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | VM name (e.g., `async-tasks-vm`) |
| `zone` | string | Availability zone |
| `boot_disk.image_id` | ref | `container-optimized-image` family |
| `network_interface.subnet_id` | ref | References VPC Subnet |
| `network_interface.nat` | bool | `true` (public IP for image pulling + SSH) |
| `service_account_id` | ref | References Service Account |
| `metadata.docker-compose` | file | Docker Compose YAML for container declaration |
| `metadata.user-data` | file | cloud-init for SSH keys |
| `resources.cores` | int | CPU cores (2) |
| `resources.memory` | int | RAM in GB (4) |

**Terraform resource**: `yandex_compute_instance`
**Data source**: `yandex_compute_image` with `family = "container-optimized-image"`

## Resource Dependency Graph

```
yandex_vpc_network
  └── yandex_vpc_subnet
        └── yandex_compute_instance (COI VM)
              ├── data.yandex_compute_image (container-optimized-image)
              ├── yandex_iam_service_account
              │     ├── iam_member: container-registry.images.puller
              │     └── iam_member: ydb.editor
              └── metadata: docker-compose.yaml
                    └── references: yandex_container_registry (image paths)
                    └── references: yandex_ydb_database_serverless (endpoint)

yandex_container_registry (independent — images pushed externally)
yandex_ydb_database_serverless (independent — schema applied via goose separately)
```

## State Transitions

This feature has no application-level state machines. The only relevant lifecycle is the Terraform resource lifecycle:

1. **Create** (`terraform apply`): All resources provisioned in dependency order
2. **Update** (`terraform apply` after config change): Modified resources updated in-place or recreated
3. **Destroy** (`terraform destroy`): All resources removed in reverse dependency order

## Validation Rules

| Rule | Enforced By |
|------|------------|
| `cloud_id` must be non-empty | `variables.tf` — no default, required |
| `folder_id` must be non-empty | `variables.tf` — no default, required |
| `zone` must be valid YC zone | Terraform provider validates on apply |
| `sa_key_file` must point to valid JSON | Terraform provider validates on init |
| Container images must exist in registry before VM boot | Documented prerequisite (build + push script) |
| Migrations must be applied before examples run | Documented prerequisite (goose up) |
