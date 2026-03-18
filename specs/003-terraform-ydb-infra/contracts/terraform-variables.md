# Contract: Terraform Input Variables

**Interface type**: Terraform variable definitions (`variables.tf`)
**Consumer**: Developer running `terraform apply`

## Required Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `cloud_id` | `string` | — (required) | Yandex Cloud organization cloud ID |
| `folder_id` | `string` | — (required) | Yandex Cloud folder ID for all resources |
| `sa_key_file` | `string` | — (required) | Path to service account key JSON file for Terraform provider auth |

## Optional Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `zone` | `string` | `"ru-central1-a"` | Yandex Cloud availability zone |
| `vm_cores` | `number` | `2` | CPU cores for the COI VM |
| `vm_memory` | `number` | `4` | RAM (GB) for the COI VM |
| `ssh_public_key` | `string` | `""` | SSH public key for VM access (optional, for debugging) |
| `ydb_name` | `string` | `"async-tasks-ydb"` | Name of the YDB Serverless database |
| `registry_name` | `string` | `"async-tasks-registry"` | Name of the container registry |

## Usage

```bash
# Via terraform.tfvars
cloud_id    = "b1g..."
folder_id   = "b1g..."
sa_key_file = "../sa.json"

# Or via CLI
terraform apply -var="cloud_id=b1g..." -var="folder_id=b1g..." -var="sa_key_file=../sa.json"

# Or via environment variables
export TF_VAR_cloud_id="b1g..."
export TF_VAR_folder_id="b1g..."
export TF_VAR_sa_key_file="../sa.json"
```
