# Quickstart: Deploying async-tasks-ydb Infrastructure

**Feature Branch**: `003-terraform-ydb-infra` | **Date**: 2026-03-17

## Prerequisites

1. **Yandex Cloud CLI** (`yc`) installed and configured
2. **Terraform** ≥ 1.5 installed
3. **Docker** installed and running
4. A Yandex Cloud **service account key** file (JSON) with permissions:
   - `editor` on the folder (for Terraform to create resources)
5. Know your **cloud ID** and **folder ID**:
   ```bash
   yc config list  # shows cloud-id, folder-id
   ```

## Step 1: Build and Push Container Images

```bash
# From repository root

# Authenticate Docker to Yandex Container Registry
cat sa.json | docker login --username json_key --password-stdin cr.yandex

# First, create the registry via Terraform (Step 2) or manually:
# yc container registry create --name async-tasks-registry

# Set your registry ID
export REGISTRY_ID=$(cd terraform && terraform output -raw registry_id)

# Build and push all examples
for example in 01_db_producer 02_cdc_worker 03_topic; do
  docker build \
    --build-arg EXAMPLE=$example \
    -t cr.yandex/$REGISTRY_ID/$example:latest \
    .
  docker push cr.yandex/$REGISTRY_ID/$example:latest
done
```

> **Note**: Images must be built and pushed before the VM can start containers. The recommended workflow is: `terraform apply` (creates registry + infra) → build & push images → VM pulls on boot or restart.

## Step 2: Provision Infrastructure

```bash
cd terraform

# Copy the example tfvars and fill in your values
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your cloud_id, folder_id, sa_key_file

# Initialize Terraform
terraform init

# Preview changes
terraform plan

# Apply
terraform apply
```

## Step 3: Apply Database Migrations

```bash
# From repository root
# The YDB endpoint is now available from Terraform output
export YDB_ENDPOINT=$(cd terraform && terraform output -raw ydb_endpoint)

# Run goose migrations
make migrate
```

## Step 4: Verify

```bash
# Get VM IP
VM_IP=$(cd terraform && terraform output -raw vm_external_ip)

# SSH into the VM (if SSH key was provided)
ssh yc-user@$VM_IP

# On the VM, check running containers
sudo docker ps

# Check container logs
sudo docker logs db-producer
sudo docker logs cdc-worker
sudo docker logs topic-bench
```

## Teardown

```bash
cd terraform
terraform destroy
```

This removes all Yandex Cloud resources. Container images in the registry are also deleted when the registry is destroyed.

## Recommended Workflow Order

1. `terraform apply` — creates registry, YDB, network, SA, VM
2. Build & push Docker images to the registry
3. `make migrate` — apply goose migrations to YDB
4. Restart VM or wait for containers to pull images:
   ```bash
   # SSH into VM then:
   sudo systemctl restart docker
   ```
5. Verify containers are running and connected to YDB
