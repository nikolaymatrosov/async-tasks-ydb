# Contract: Terraform Output Values

**Interface type**: Terraform output definitions (`outputs.tf`)
**Consumer**: Developer, CI/CD pipelines, helper scripts

## Outputs

| Output | Type | Description |
|--------|------|-------------|
| `ydb_endpoint` | `string` | Full gRPC connection string for YDB (e.g., `grpcs://lb.<id>.ydb.mdb.yandexcloud.net:2135/?database=...`) |
| `ydb_database_path` | `string` | YDB database path (e.g., `/ru-central1/<cloud>/<db>`) |
| `registry_id` | `string` | Container Registry ID (used in image paths: `cr.yandex/<id>/...`) |
| `vm_external_ip` | `string` | Public IP of the COI VM (for SSH access) |
| `vm_internal_ip` | `string` | Private IP of the COI VM |
| `service_account_id` | `string` | ID of the created service account |

## Usage

```bash
# After terraform apply:
terraform output ydb_endpoint
terraform output vm_external_ip

# SSH into VM:
ssh yc-user@$(terraform output -raw vm_external_ip)

# Use YDB endpoint in .env:
echo "YDB_ENDPOINT=$(terraform output -raw ydb_endpoint)" > ../.env
```
