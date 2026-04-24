# Implementation Plan: Terraform API Gateway Module

**Branch**: `011-apigw-terraform-module` | **Date**: 2026-04-24 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/011-apigw-terraform-module/spec.md`

## Summary

Add a reusable `terraform/modules/apigw/` module that wraps the `yandex_api_gateway` Yandex Cloud resource, accepting an OpenAPI 3.0 spec string and standard identity inputs, and exposing `gateway_id` and `gateway_domain` outputs. Wire the module into the existing root `terraform/` configuration by adding three new variables and two new outputs, following the identical file layout used by the `db`, `workers`, and `producer` modules.

## Technical Context

**Language/Version**: HCL (Terraform ≥ 1.5)
**Primary Dependencies**: `yandex-cloud/yandex` provider — already declared in `terraform/main.tf` and all existing module `versions.tf` files; no new provider additions required
**Storage**: N/A — no YDB schema changes; no new tables or topics
**Testing**: `terraform validate` + `terraform plan` against a real Yandex Cloud folder
**Target Platform**: Yandex Cloud (managed API Gateway service)
**Project Type**: Terraform infrastructure module
**Performance Goals**: N/A
**Constraints**: No hardcoded credentials, folder IDs, or endpoints; all values passed as variables
**Scale/Scope**: One new module (4 files) + root config additions (3 files touched)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | N/A — HCL module, not a Go example |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | N/A — HCL module |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ — no schema changes |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ — all values passed as Terraform variables; no hardcoding |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | N/A — HCL module |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | N/A — no Go code added |

Post-design re-check: ✅ Design confirms no deviations from applicable principles.

## Project Structure

### Documentation (this feature)

```text
specs/011-apigw-terraform-module/
├── plan.md         ← this file
├── research.md     ← Phase 0 output
├── data-model.md   ← Phase 1 output
└── tasks.md        ← Phase 2 output (/speckit-tasks command)
```

### Source Code Changes

```text
terraform/
├── main.tf                   ← add module "apigw" block
├── variables.tf              ← add apigw_name, apigw_description, apigw_spec_file
├── outputs.tf                ← add gateway_id, gateway_domain
└── modules/
    └── apigw/                ← new module (4 new files)
        ├── versions.tf
        ├── variables.tf
        ├── main.tf
        └── outputs.tf
```

**Structure Decision**: New `terraform/modules/apigw/` follows the identical 4-file layout of `db`, `workers`, and `producer` modules. Root module receives 3 new variables and 2 new outputs. No other directories are created or modified.

## Complexity Tracking

| Violation                                                          | Why Needed                                                                                                                                                       | Simpler Alternative Rejected Because                                                 |
|--------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------|
| Constitution principles I, II, V, Tech Constraints not applicable  | Feature is a Terraform HCL module; constitution is scoped to Go examples by its own language constraint ("Go 1.26", "go.mod", `slog`, `signal.NotifyContext`)    | Constitution cannot be amended per feature; documented deviation is the correct path |

## Implementation Notes

### terraform/modules/apigw/versions.tf

```hcl
terraform {
  required_providers {
    yandex = {
      source = "yandex-cloud/yandex"
    }
  }
}
```

### terraform/modules/apigw/variables.tf

```hcl
variable "name" {
  description = "Name of the API Gateway"
  type        = string
}

variable "description" {
  description = "Human-readable description of the API Gateway"
  type        = string
  default     = ""
}

variable "folder_id" {
  description = "Yandex Cloud folder ID for the API Gateway resource"
  type        = string
}

variable "spec" {
  description = "OpenAPI 3.0 YAML specification content (pre-read string)"
  type        = string
}

variable "labels" {
  description = "Key-value labels to attach to the API Gateway"
  type        = map(string)
  default     = {}
}
```

### terraform/modules/apigw/main.tf

```hcl
resource "yandex_api_gateway" "main" {
  name        = var.name
  description = var.description
  folder_id   = var.folder_id
  spec        = var.spec
  labels      = var.labels
}
```

### terraform/modules/apigw/outputs.tf

```hcl
output "gateway_id" {
  description = "API Gateway resource ID"
  value       = yandex_api_gateway.main.id
}

output "gateway_domain" {
  description = "Default domain assigned to the API Gateway"
  value       = yandex_api_gateway.main.domain
}
```

### terraform/main.tf addition

```hcl
module "apigw" {
  source = "./modules/apigw"

  name        = var.apigw_name
  description = var.apigw_description
  folder_id   = var.folder_id
  spec        = file("${path.module}/${var.apigw_spec_file}")
}
```

### terraform/variables.tf additions

```hcl
variable "apigw_name" {
  description = "Name of the API Gateway"
  type        = string
  default     = "async-tasks-apigw"
}

variable "apigw_description" {
  description = "Description of the API Gateway"
  type        = string
  default     = ""
}

variable "apigw_spec_file" {
  description = "Path to the OpenAPI 3.0 spec YAML, relative to the terraform/ directory"
  type        = string
  default     = "apigw-spec.yaml"
}
```

### terraform/outputs.tf additions

```hcl
output "gateway_id" {
  description = "API Gateway resource ID"
  value       = module.apigw.gateway_id
}

output "gateway_domain" {
  description = "Default domain assigned to the API Gateway"
  value       = module.apigw.gateway_domain
}
```

### terraform/apigw-spec.yaml (placeholder)

A minimal valid OpenAPI 3.0 stub is added so `terraform validate` passes without a real spec:

```yaml
openapi: "3.0.0"
info:
  title: async-tasks API
  version: "1.0"
x-yc-apigateway:
  service_account_id: ""
paths: {}
```
