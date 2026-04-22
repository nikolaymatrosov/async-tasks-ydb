# Terraform Module Interface Contracts

Each child module exposes a declared variable and output interface. The root module is the sole consumer of these interfaces; no external caller should invoke child modules directly.

---

## Contract: `module.db`

**Source**: `./modules/db`  
**Purpose**: Provision shared foundational infrastructure (network, YDB, registry, IAM).

### Required Inputs (no default)

```hcl
variable "cloud_id"  { type = string }  # Yandex Cloud cloud ID
variable "folder_id" { type = string }  # Yandex Cloud folder ID
```

### Optional Inputs (with defaults)

```hcl
variable "zone"                { type = string        default = "ru-central1-a" }
variable "subnet_cidrs"        { type = map(string)   default = { "ru-central1-a" = "10.128.0.0/24", ... } }
variable "ydb_name"            { type = string        default = "async-tasks-ydb" }
variable "ydb_resource_preset" { type = string        default = "medium" }
variable "ydb_fixed_size"      { type = number        default = 1 }
variable "ydb_storage_type"    { type = string        default = "ssd" }
variable "ydb_storage_groups"  { type = number        default = 1 }
variable "registry_name"       { type = string        default = "async-tasks-registry" }
```

### Outputs

```hcl
output "ydb_endpoint"        { value = yandex_ydb_database_dedicated.main.ydb_full_endpoint }
output "ydb_database_path"   { value = yandex_ydb_database_dedicated.main.database_path }
output "registry_id"         { value = yandex_container_registry.main.id }
output "registry_url"        { value = "cr.yandex/${yandex_container_registry.main.id}" }
output "service_account_id"  { value = yandex_iam_service_account.coi_vm.id }
output "subnet_ids"          { value = [for s in yandex_vpc_subnet.main : s.id] }
output "network_id"          { value = yandex_vpc_network.main.id }
```

### Invariants

- All IAM role bindings for the shared service account are created within this module.
- The container registry is created here and its ID is exported for image URL construction.
- No compute resources (VMs, instance groups) are created by this module.

---

## Contract: `module.workers`

**Source**: `./modules/workers`  
**Purpose**: Build worker container images and deploy the autoscaling worker instance group.

### Required Inputs (no default — must come from `module.db` outputs)

```hcl
variable "folder_id"          { type = string }
variable "registry_url"       { type = string }        # cr.yandex/<id>
variable "service_account_id" { type = string }
variable "ydb_endpoint"       { type = string }
variable "ydb_database"       { type = string }
variable "subnet_ids"         { type = list(string) }
```

### Optional Inputs (with defaults)

```hcl
variable "zone"                     { type = string  default = "ru-central1-a" }
variable "platform_id"              { type = string  default = "standard-v4a" }
variable "vm_cores"                 { type = number  default = 2 }
variable "vm_memory"                { type = number  default = 4 }
variable "ssh_public_key"           { type = string  default = "" }
variable "ig_max_size"              { type = number  default = 5 }
variable "ig_min_zone_size"         { type = number  default = 1 }
variable "ig_cpu_target"            { type = number  default = 70 }
variable "ig_stabilization_duration"{ type = number  default = 300 }
variable "ig_warmup_duration"       { type = number  default = 120 }
variable "ig_measurement_duration"  { type = number  default = 60 }
variable "worker_rate"              { type = number  default = 115 }
```

### Outputs

```hcl
output "instance_group_id"   { value = yandex_compute_instance_group.workers.id }
output "coordinator_image"   { value = local.coordinator_image }
output "cdc_worker_image"    { value = local.cdc_worker_image }
output "topic_bench_image"   { value = local.topic_bench_image }
output "migrations_image"    { value = local.migrations_image }
output "migrations_run_cmd"  { value = "docker run --rm ${local.migrations_image} ..." }
```

### Invariants

- Container image builds are gated on `data.external.git_hash.result.sha` — re-deploy only when git HEAD changes.
- The instance group uses CPU-based autoscaling.
- The `folder_id` variable is not used to look up IAM or network resources; it is passed through for resource tags only.

---

## Contract: `module.producer`

**Source**: `./modules/producer`  
**Purpose**: Build the db-producer container image and deploy a fixed-scale producer instance group.

### Required Inputs (no default — must come from `module.db` outputs)

```hcl
variable "folder_id"          { type = string }
variable "registry_url"       { type = string }        # cr.yandex/<id>
variable "service_account_id" { type = string }
variable "ydb_endpoint"       { type = string }
variable "ydb_database"       { type = string }
variable "subnet_ids"         { type = list(string) }
```

### Optional Inputs (with defaults)

```hcl
variable "zone"                { type = string  default = "ru-central1-a" }
variable "platform_id"         { type = string  default = "standard-v4a" }
variable "vm_cores"            { type = number  default = 2 }
variable "vm_memory"           { type = number  default = 4 }
variable "ssh_public_key"      { type = string  default = "" }
variable "producer_size"       { type = number  default = 1 }
variable "producer_parallelism"{ type = number  default = 10 }
```

### Outputs

```hcl
output "producer_instance_group_id" { value = yandex_compute_instance_group.producer.id }
output "db_producer_image"          { value = local.db_producer_image }
```

### Invariants

- Scale policy is `fixed_scale { size = var.producer_size }` — not autoscaling.
- Container image build is gated on `data.external.git_hash.result.sha`.
- The producer VM uses `yc.WithMetadataCredentials()` (no SA key file mounted) — the instance's IAM identity provides YDB access.

---

## Root Module Composition

```hcl
# terraform/main.tf

terraform {
  required_version = ">= 1.5"
  required_providers { ... }
}

provider "yandex" {
  cloud_id  = var.cloud_id
  folder_id = var.folder_id
  zone      = var.zone
}

module "db" {
  source           = "./modules/db"
  cloud_id         = var.cloud_id
  folder_id        = var.folder_id
  zone             = var.zone
  subnet_cidrs     = var.subnet_cidrs
  ydb_name         = var.ydb_name
  # ... other db vars
}

module "workers" {
  source             = "./modules/workers"
  folder_id          = var.folder_id
  zone               = var.zone
  # ... worker vars ...
  registry_url       = module.db.registry_url
  service_account_id = module.db.service_account_id
  ydb_endpoint       = module.db.ydb_endpoint
  ydb_database       = module.db.ydb_database_path
  subnet_ids         = module.db.subnet_ids
}

module "producer" {
  source             = "./modules/producer"
  folder_id          = var.folder_id
  zone               = var.zone
  # ... producer vars ...
  registry_url       = module.db.registry_url
  service_account_id = module.db.service_account_id
  ydb_endpoint       = module.db.ydb_endpoint
  ydb_database       = module.db.ydb_database_path
  subnet_ids         = module.db.subnet_ids
}
```

## Root Module Backward-Compatible Outputs

All outputs from the current flat `terraform/outputs.tf` are preserved in the root module by delegating to child module outputs:

| Root Output | Source |
| ----------- | ------ |
| `ydb_endpoint` | `module.db.ydb_endpoint` |
| `ydb_database_path` | `module.db.ydb_database_path` |
| `registry_id` | `module.db.registry_id` |
| `instance_group_id` | `module.workers.instance_group_id` |
| `service_account_id` | `module.db.service_account_id` |
| `db_producer_image` | `module.producer.db_producer_image` |
| `cdc_worker_image` | `module.workers.cdc_worker_image` |
| `topic_bench_image` | `module.workers.topic_bench_image` |
| `coordinator_image` | `module.workers.coordinator_image` |
| `migrations_image` | `module.workers.migrations_image` |
| `migrations_run_cmd` | `module.workers.migrations_run_cmd` |
| `producer_instance_group_id` *(new)* | `module.producer.producer_instance_group_id` |
