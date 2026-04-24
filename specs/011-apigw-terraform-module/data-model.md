# Data Model: Terraform API Gateway Module

**Feature**: 011-apigw-terraform-module
**Date**: 2026-04-24

---

## Module: `terraform/modules/apigw`

### Inputs (variables.tf)

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `name` | `string` | yes | — | Name of the API Gateway resource |
| `description` | `string` | no | `""` | Human-readable description |
| `folder_id` | `string` | yes | — | Yandex Cloud folder ID |
| `spec` | `string` | yes | — | OpenAPI 3.0 YAML spec content (pre-read string) |
| `labels` | `map(string)` | no | `{}` | Key-value labels to attach to the resource |

### Outputs (outputs.tf)

| Output | Type | Description |
|--------|------|-------------|
| `gateway_id` | `string` | The resource ID of the created API Gateway |
| `gateway_domain` | `string` | Auto-assigned default domain (`<id>.apigw.yandexcloud.net`) |

---

## Root module additions (terraform/)

### New variables (variables.tf additions)

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `apigw_name` | `string` | no | `"async-tasks-apigw"` | Name of the API Gateway |
| `apigw_description` | `string` | no | `""` | Description of the API Gateway |
| `apigw_spec_file` | `string` | no | `"apigw-spec.yaml"` | Path to the OpenAPI 3.0 spec YAML file, relative to the terraform/ directory |

### New outputs (outputs.tf additions)

| Output | Description |
|--------|-------------|
| `gateway_id` | API Gateway resource ID |
| `gateway_domain` | Default domain assigned to the gateway |

---

## Resource graph

```
terraform/main.tf
  └── module "apigw"
        ├── inputs: folder_id (from var), name, description, spec (from file())
        └── outputs: gateway_id, gateway_domain → terraform/outputs.tf
```

No cross-module dependencies: the `apigw` module is independent of `db`, `workers`, and `producer` modules. Future integration (e.g., passing `ydb_endpoint` into the spec via `templatefile()`) is out of scope for this feature.
