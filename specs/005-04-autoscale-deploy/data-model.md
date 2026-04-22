# Data Model: 005-04-autoscale-deploy

This feature adds no new YDB schema. The `coordinated_tasks` table (managed by existing migrations) is unchanged. The relevant "data models" for this feature are the metrics schema and the Terraform configuration entities.

---

## Prometheus Metrics Schema

Exposed by the Go binary at `GET /metrics` (Prometheus text format).

All metrics carry a `worker_id` label (UUID, auto-generated per process) so per-instance and aggregate views are possible in Yandex Monitoring.

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `coordinator_tasks_processed_total` | counter | `worker_id` | Cumulative tasks marked completed |
| `coordinator_tasks_locked_total` | counter | `worker_id` | Cumulative tasks locked (attempt counter) |
| `coordinator_tasks_errors_total` | counter | `worker_id` | Cumulative failed lock/complete operations |
| `coordinator_partitions_owned` | gauge | `worker_id` | Current number of owned partitions |
| `coordinator_up` | gauge | `worker_id` | 1 if the worker is running, 0 otherwise |

Producer mode exposes only `coordinator_up` (value 1) so the metrics endpoint is always present regardless of `--mode`.

---

## Terraform Configuration Entities

### Instance Group

| Field | Type | Default | Purpose |
|---|---|---|---|
| `ig_min_zone_size` | number | 1 | Minimum instances per zone |
| `ig_max_size` | number | 5 | Hard ceiling on total group size |
| `ig_cpu_target` | number | 70 | CPU % trigger for scale-out |
| `ig_stabilization_duration` | number | 300 | Seconds before scale-in permitted |
| `ig_warmup_duration` | number | 120 | Seconds new instance is excluded from autoscale |
| `ig_measurement_duration` | number | 60 | Averaging window in seconds |

### Docker-Compose Template Variables

Injected into `terraform/docker-compose.yml.tpl` by `templatefile()`:

| Variable | Source | Purpose |
|---|---|---|
| `coordinator_image` | `local.coordinator_image` | App container image URL with git SHA tag |
| `ydb_endpoint` | `var.ydb_endpoint` | YDB gRPC endpoint |
| `ydb_database` | `var.ydb_database` | YDB database path |
| `folder_id` | `var.folder_id` | Yandex Cloud folder for Monitoring writes |
| `metrics_port` | hardcoded `9090` | Port the app exposes `/metrics` on |

### Unified Agent Config Template Variables

Injected into `terraform/ua-config.yml.tpl`:

| Variable | Source | Purpose |
|---|---|---|
| `folder_id` | `var.folder_id` | Yandex Monitoring target folder |
| `metrics_url` | derived `http://localhost:${metrics_port}/metrics` | Prometheus scrape target |

---

## State Transitions (none new)

The `coordinated_tasks` table state machine (`pending → locked → completed`) is unchanged. This feature only adds observability and deployment infrastructure around the existing worker.
