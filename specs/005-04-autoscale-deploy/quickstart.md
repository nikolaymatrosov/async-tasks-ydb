# Quickstart: 005-04-autoscale-deploy

## Prerequisites

- Terraform ≥ 1.5 installed
- Docker available locally (for `terraform/containers.tf` image builds)
- Yandex Cloud service account key (`sa.json`) with roles: `container-registry.images.puller`, `ydb.editor`, `monitoring.editor`
- YDB database and topics already provisioned (see earlier terraform state or `migrations/`)
- `terraform.tfvars` populated (copy from `terraform.tfvars.example`)

## 1. Build and push the coordinator image

```bash
# From repo root
docker build --platform linux/amd64 --build-arg EXAMPLE=04_coordinated_table \
  -t <registry_url>/coordinator:<git_sha> .
docker push <registry_url>/coordinator:<git_sha>
```

Or let Terraform do it automatically via `null_resource.coordinator_image` during `terraform apply`.

## 2. Configure tfvars

Add to `terraform/terraform.tfvars`:

```hcl
ydb_endpoint  = "grpcs://ydb.serverless.yandexcloud.net:2135"
ydb_database  = "/ru-central1/<cloud_id>/<db_id>"
folder_id     = "<your_folder_id>"

# Optional: tune autoscaling
ig_max_size    = 5
ig_cpu_target  = 70
worker_rate    = 115
```

## 3. Apply infrastructure

```bash
cd terraform
terraform init
terraform apply
```

Terraform will:
1. Build and push the coordinator Docker image (if triggered by git SHA change)
2. Add `monitoring.editor` IAM binding to the service account
3. Create (or replace) the instance group with 1 initial instance
4. Each instance boots COI, reads the `docker-compose` metadata key, and starts two containers:
   - `coordinator` — the `04_coordinated_table` binary in `--mode worker --rate 115`
   - `unified-agent` — scrapes `/metrics` and forwards to Yandex Monitoring

## 4. Verify metrics are flowing

In Yandex Monitoring (console.yandex.cloud), navigate to your folder → Monitoring → Metrics Explorer. Query:

```
coordinator_tasks_processed_total{worker_id=~".*"}
```

Metrics should appear within ~30 seconds of instance startup.

## 5. Run load test

Start a producer (from any machine with YDB access):

```bash
go run ./04_coordinated_table/ \
  --endpoint $YDB_ENDPOINT \
  --database $YDB_DATABASE \
  --mode producer \
  --rate 115
```

Or add a producer instance to the instance group by setting `worker_rate` to 0 and deploying a separate producer instance group (not in scope for this feature; the producer can run locally or on a separate VM).

## 6. Observe autoscaling

Apply sustained load above ~70% CPU for 60 seconds. The instance group will add instances. Watch:

```bash
yc compute instance-group list-instances --id <group_id>
```

Or observe in the Yandex Cloud console under Compute → Instance Groups.

## 7. Validate throughput

After 30 minutes of 115 RPS load, verify in Yandex Monitoring:

- `coordinator_tasks_processed_total` rate ≥ 115/s across all instances
- `coordinator_tasks_errors_total` rate < 1.15/s (< 1% of throughput)

## Expected slog output (worker mode, healthy)

```json
{"time":"...","level":"INFO","msg":"worker starting","worker_id":"<uuid>"}
{"time":"...","level":"INFO","msg":"coordination node ready","path":"/..."}
{"time":"...","level":"INFO","msg":"metrics server started","addr":":9090"}
{"time":"...","level":"INFO","msg":"worker stats","worker_id":"<uuid>","partitions_owned":12,"tasks_processed":1150,"tasks_locked":1155,"uptime":"10s"}
```
