# Quickstart: Terraform Modular Deployment

## Prerequisites

- Terraform ≥ 1.5 installed
- `yc` CLI authenticated with the target cloud folder
- Docker available locally (for container image builds)
- Existing `terraform/terraform.tfvars` (no changes required to existing vars)

## Deploying the Full Stack

```bash
cd terraform/
terraform init
terraform apply
```

All three modules deploy in dependency order: `db` first, then `workers` and `producer` in parallel.

---

## Deploying the Database Only

Use this for initial environment setup or database-only changes (scaling, storage).

```bash
cd terraform/
terraform apply -target=module.db
```

**Verifies**: YDB endpoint is reachable; no compute instance groups are created.

---

## Deploying Workers Only

Requires the database to already be deployed.

```bash
cd terraform/
terraform apply -target=module.workers
```

**Verifies**: `terraform output instance_group_id` returns a non-empty string. Workers connect to the existing YDB endpoint.

---

## Deploying the Producer Only

Requires the database to already be deployed.

```bash
cd terraform/
terraform apply -target=module.producer
```

**Verifies**: `terraform output producer_instance_group_id` returns a non-empty string. Producer VMs write tasks to YDB.

---

## New Variables (Optional — existing tfvars unchanged)

Add to `terraform.tfvars` if you need to override defaults:

```hcl
# Producer instance group
producer_size        = 1   # number of producer VMs (default: 1)
producer_parallelism = 10  # --parallelism flag passed to db-producer (default: 10)
```

---

## Destroying a Component

```bash
# Destroy only the producer (leaves database and workers intact)
terraform destroy -target=module.producer

# Destroy only the workers (leaves database and producer intact)
terraform destroy -target=module.workers

# Destroy everything
terraform destroy
```

**Note**: The database (`module.db`) should be destroyed last. Destroying it while workers or producers still exist will leave orphaned compute resources referencing a deleted YDB endpoint.

---

## Checking Module Outputs

```bash
# All outputs
terraform output

# Specific output
terraform output ydb_endpoint
terraform output producer_instance_group_id
```

---

## Error: Database Not Yet Deployed

If you run `terraform apply -target=module.workers` before deploying the database, Terraform will error with something like:

```
Error: Reference to undeclared resource
  module.db.registry_url is not yet known
```

Fix: deploy the database first with `terraform apply -target=module.db`, then re-run the workers apply.
