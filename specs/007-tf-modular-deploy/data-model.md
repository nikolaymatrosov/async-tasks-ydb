# Data Model: Terraform Modular Deployment

This document describes the Terraform module entities, their resource ownership, variable interfaces, and output contracts.

## Module Dependency Graph

```
root module (terraform/)
├── module.db          — no upstream dependencies
├── module.workers     — depends on module.db outputs
└── module.producer    — depends on module.db outputs
```

`module.workers` and `module.producer` are independent of each other. Both depend on `module.db` for shared infrastructure outputs.

---

## Module: `db` (`terraform/modules/db/`)

Owns all shared foundational resources. Must be deployed first.

### Resources Owned

| Resource | Type | Notes |
| -------- | ---- | ----- |
| `yandex_vpc_network.main` | Network | `async-tasks-net` |
| `yandex_vpc_subnet.main` | Subnet (for_each zone) | One subnet per availability zone |
| `yandex_ydb_database_dedicated.main` | YDB Dedicated | `async-tasks-ydb`; depends on network + subnets |
| `yandex_container_registry.main` | Container Registry | `async-tasks-registry`; shared by workers + producer |
| `yandex_iam_service_account.coi_vm` | IAM Service Account | `coi-vm-sa`; used by workers + producer VMs |
| `yandex_resourcemanager_folder_iam_member.*` | IAM bindings (×7) | registry.puller, ydb.editor, monitoring.editor, vpc.user, vpc.publicAdmin, compute.editor, iam.serviceAccounts.user |
| `data.dirhash_sha256.migrations` | Data | Hash of `../migrations/` directory |

### Input Variables

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `cloud_id` | `string` | — | Yandex Cloud organization cloud ID |
| `folder_id` | `string` | — | Yandex Cloud folder ID |
| `zone` | `string` | `ru-central1-a` | Default availability zone |
| `subnet_cidrs` | `map(string)` | 3-zone map | Zone → CIDR mapping |
| `ydb_name` | `string` | `async-tasks-ydb` | YDB database name |
| `ydb_resource_preset` | `string` | `medium` | YDB compute preset |
| `ydb_fixed_size` | `number` | `1` | YDB node count |
| `ydb_storage_type` | `string` | `ssd` | YDB storage type |
| `ydb_storage_groups` | `number` | `1` | YDB storage group count |
| `registry_name` | `string` | `async-tasks-registry` | Container registry name |

### Outputs

| Output | Type | Description |
| ------ | ---- | ----------- |
| `ydb_endpoint` | `string` | Full gRPC connection string (`ydb_full_endpoint`) |
| `ydb_database_path` | `string` | YDB database path |
| `registry_id` | `string` | Container registry ID |
| `registry_url` | `string` | `cr.yandex/<registry_id>` — base URL for image tags |
| `service_account_id` | `string` | COI VM service account ID |
| `subnet_ids` | `list(string)` | All subnet IDs (passed to workers + producer) |
| `network_id` | `string` | VPC network ID |

---

## Module: `workers` (`terraform/modules/workers/`)

Owns the worker compute instance group and related container image builds.

### Resources Owned

| Resource | Type | Notes |
| -------- | ---- | ----- |
| `data.yandex_compute_image.coi` | Data | Container-Optimized Image family |
| `data.external.git_hash` | Data | Current HEAD SHA for image tagging |
| `null_resource.cdc_worker_image` | Build | `02_cdc_worker` docker build + push |
| `null_resource.coordinator_image` | Build | `04_coordinated_table` docker build + push |
| `null_resource.topic_bench_image` | Build | `03_topic` docker build + push |
| `null_resource.migrations_image` | Build | `Dockerfile.migrations` build + push |
| `yandex_compute_instance_group.workers` | Compute IG | Autoscaling; runs coordinator container |

### Input Variables

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `folder_id` | `string` | — | Yandex Cloud folder ID |
| `zone` | `string` | `ru-central1-a` | Allocation zone |
| `platform_id` | `string` | `standard-v4a` | VM platform |
| `vm_cores` | `number` | `2` | VM CPU cores |
| `vm_memory` | `number` | `4` | VM RAM (GB) |
| `ssh_public_key` | `string` | `""` | Optional SSH key |
| `ig_max_size` | `number` | `5` | Max autoscale size |
| `ig_min_zone_size` | `number` | `1` | Min per zone |
| `ig_cpu_target` | `number` | `70` | CPU% scale-out target |
| `ig_stabilization_duration` | `number` | `300` | Scale-in delay (s) |
| `ig_warmup_duration` | `number` | `120` | New-instance exclusion (s) |
| `ig_measurement_duration` | `number` | `60` | Averaging window (s) |
| `worker_rate` | `number` | `115` | Task rate flag for coordinator |
| `registry_url` | `string` | — | From `module.db.registry_url` |
| `service_account_id` | `string` | — | From `module.db.service_account_id` |
| `ydb_endpoint` | `string` | — | From `module.db.ydb_endpoint` |
| `ydb_database` | `string` | — | From `module.db.ydb_database_path` |
| `subnet_ids` | `list(string)` | — | From `module.db.subnet_ids` |

### Outputs

| Output | Type | Description |
| ------ | ---- | ----------- |
| `instance_group_id` | `string` | Worker IG resource ID |
| `coordinator_image` | `string` | Full image reference with SHA tag |
| `cdc_worker_image` | `string` | Full image reference with SHA tag |
| `topic_bench_image` | `string` | Full image reference with SHA tag |
| `migrations_image` | `string` | Full image reference with SHA tag |
| `migrations_run_cmd` | `string` | Ready-to-use `docker run` command |

---

## Module: `producer` (`terraform/modules/producer/`)

Owns the producer compute instance group and the `db-producer` container image build.

### Resources Owned

| Resource | Type | Notes |
| -------- | ---- | ----- |
| `data.yandex_compute_image.coi` | Data | Container-Optimized Image family |
| `data.external.git_hash` | Data | Current HEAD SHA for image tagging |
| `null_resource.db_producer_image` | Build | `01_db_producer` docker build + push |
| `yandex_compute_instance_group.producer` | Compute IG | Fixed-scale; runs db-producer container |

### Input Variables

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `folder_id` | `string` | — | Yandex Cloud folder ID |
| `zone` | `string` | `ru-central1-a` | Allocation zone |
| `platform_id` | `string` | `standard-v4a` | VM platform |
| `vm_cores` | `number` | `2` | VM CPU cores |
| `vm_memory` | `number` | `4` | VM RAM (GB) |
| `ssh_public_key` | `string` | `""` | Optional SSH key |
| `producer_size` | `number` | `1` | Fixed size of producer instance group |
| `producer_parallelism` | `number` | `10` | Maps to `--parallelism` in db-producer |
| `registry_url` | `string` | — | From `module.db.registry_url` |
| `service_account_id` | `string` | — | From `module.db.service_account_id` |
| `ydb_endpoint` | `string` | — | From `module.db.ydb_endpoint` |
| `ydb_database` | `string` | — | From `module.db.ydb_database_path` |
| `subnet_ids` | `list(string)` | — | From `module.db.subnet_ids` |

### Outputs

| Output | Type | Description |
| ------ | ---- | ----------- |
| `producer_instance_group_id` | `string` | Producer IG resource ID |
| `db_producer_image` | `string` | Full image reference with SHA tag |

---

## Root Module Variable → Child Module Mapping

| Root Variable | Passed to `db` | Passed to `workers` | Passed to `producer` |
| ------------- | -------------- | ------------------- | -------------------- |
| `cloud_id` | ✅ | — | — |
| `folder_id` | ✅ | ✅ | ✅ |
| `zone` | ✅ | ✅ | ✅ |
| `subnet_cidrs` | ✅ | — | — |
| `ydb_name` | ✅ | — | — |
| `ydb_resource_preset` | ✅ | — | — |
| `ydb_fixed_size` | ✅ | — | — |
| `ydb_storage_type` | ✅ | — | — |
| `ydb_storage_groups` | ✅ | — | — |
| `registry_name` | ✅ | — | — |
| `platform_id` | — | ✅ | ✅ |
| `vm_cores` | — | ✅ | ✅ |
| `vm_memory` | — | ✅ | ✅ |
| `ssh_public_key` | — | ✅ | ✅ |
| `ig_max_size` | — | ✅ | — |
| `ig_min_zone_size` | — | ✅ | — |
| `ig_cpu_target` | — | ✅ | — |
| `ig_stabilization_duration` | — | ✅ | — |
| `ig_warmup_duration` | — | ✅ | — |
| `ig_measurement_duration` | — | ✅ | — |
| `worker_rate` | — | ✅ | — |
| `producer_size` *(new)* | — | — | ✅ |
| `producer_parallelism` *(new)* | — | — | ✅ |

Root also wires `module.db` outputs to `module.workers` and `module.producer`:

```hcl
# in root main.tf — db outputs wired to workers
module "workers" {
  ...
  registry_url       = module.db.registry_url
  service_account_id = module.db.service_account_id
  ydb_endpoint       = module.db.ydb_endpoint
  ydb_database       = module.db.ydb_database_path
  subnet_ids         = module.db.subnet_ids
}

# and to producer
module "producer" {
  ...
  registry_url       = module.db.registry_url
  service_account_id = module.db.service_account_id
  ydb_endpoint       = module.db.ydb_endpoint
  ydb_database       = module.db.ydb_database_path
  subnet_ids         = module.db.subnet_ids
}
```
