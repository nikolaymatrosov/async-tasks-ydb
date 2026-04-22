# Contract: Terraform Variable Interface

**Module**: `terraform/`  
**Applies to**: Feature 005 additions to existing Terraform configuration

## New Required Variables

| Variable | Type | Description |
|---|---|---|
| `ydb_endpoint` | string | YDB gRPC endpoint (e.g. `grpcs://ydb.serverless.yandexcloud.net:2135`) |
| `ydb_database` | string | YDB database path (e.g. `/ru-central1/b1g.../etnXXX`) |

These were previously hardcoded in the docker-compose definition; this feature extracts them as Terraform variables so instances can be reconfigured without rebuilding images.

## New Optional Variables (with defaults)

| Variable | Type | Default | Description |
|---|---|---|---|
| `ig_max_size` | number | 5 | Maximum instance group size |
| `ig_min_zone_size` | number | 1 | Minimum instances per availability zone |
| `ig_cpu_target` | number | 70 | CPU utilisation target (%) for scale-out |
| `ig_stabilization_duration` | number | 300 | Seconds to wait before allowing scale-in |
| `ig_warmup_duration` | number | 120 | Seconds a new instance is excluded from autoscale averaging |
| `worker_rate` | number | 115 | Producer task injection rate (tasks/second) |

## Removed Resources

| Resource | Replacement |
|---|---|
| `yandex_compute_instance.coi_vm` | `yandex_compute_instance_group.workers` |

The single-instance resource is replaced by the instance group. All other existing resources (`yandex_vpc_*`, `yandex_iam_*`, `yandex_container_registry`, `null_resource.*_image`, `yandex_ydb_*`) are unchanged.

## New Outputs

| Output | Description |
|---|---|
| `instance_group_id` | ID of the created instance group (for Yandex Cloud console navigation) |

## Backward Compatibility

Removing `yandex_compute_instance.coi_vm` and adding `yandex_compute_instance_group.workers` is a **destructive change** — `terraform apply` will destroy the old VM before creating the group. This is acceptable for a load-test environment. If the old VM must be preserved, the plan must be applied with `terraform state rm` first.
