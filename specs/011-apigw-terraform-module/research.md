# Research: Terraform API Gateway Module

**Feature**: 011-apigw-terraform-module
**Date**: 2026-04-24

---

## Decision: yandex_api_gateway resource schema

**Decision**: Use `yandex_api_gateway` resource from `yandex-cloud/yandex` provider.

**Key attributes**:
```hcl
resource "yandex_api_gateway" "main" {
  name        = var.name           # required
  description = var.description    # optional
  folder_id   = var.folder_id      # optional (inherits provider default if omitted)
  spec        = var.spec           # required — OpenAPI 3.0 YAML string
  labels      = var.labels         # optional map
}
```

**Key outputs from resource**:
- `.id` → the gateway ID
- `.domain` → the auto-assigned default domain (`<id>.apigw.yandexcloud.net`)

**Rationale**: Provider is already declared in the project's root `terraform/main.tf` and all module `versions.tf` files; no new provider addition needed.

**Alternatives considered**: None — `yandex_api_gateway` is the only resource for this purpose in the provider.

---

## Decision: Module file structure

**Decision**: Follow exact same layout as `terraform/modules/db/` and `terraform/modules/workers/`:

```text
terraform/modules/apigw/
├── versions.tf   — required_providers block (yandex only)
├── variables.tf  — all inputs with descriptions and types
├── outputs.tf    — gateway_id, gateway_domain
└── main.tf       — yandex_api_gateway resource
```

**Rationale**: Consistency with existing modules means the root caller doesn't need to learn new conventions. All existing modules use this 4-file layout.

**Alternatives considered**: Single-file module — rejected because it diverges from project convention.

---

## Decision: OpenAPI spec delivery

**Decision**: Accept the spec as a `string` variable (`var.spec`). Callers use `file("path/to/spec.yaml")` or an inline heredoc. The module does not read files itself.

**Rationale**: Terraform modules cannot reliably reference files relative to the caller's working directory via `file()` — the path would be relative to the module, not the root. Accepting a pre-read string keeps the module simple and the caller in control of the spec source.

**Alternatives considered**:
- `spec_file` variable with `file()` inside the module — rejected; path resolution is module-relative and breaks caller ergonomics.
- `templatefile()` inside module — rejected; unnecessary complexity.

---

## Decision: Root integration

**Decision**: Add `module "apigw"` block to `terraform/main.tf`, gateway variables to `terraform/variables.tf`, and `gateway_id` / `gateway_domain` to `terraform/outputs.tf`. The `spec` variable in the root is populated via `file(var.apigw_spec_file)` where `apigw_spec_file` is a new root variable pointing to the YAML path.

**Rationale**: Keeps the OpenAPI spec as an external file (versioned alongside Terraform configs) rather than inline HCL.

**Alternatives considered**: Hardcoding a spec path — rejected; must be configurable per environment.

---

## Decision: Constitution applicability

**Decision**: Constitution principles I–V and the tech constraints are Go-example–focused and do not apply to HCL Terraform modules. The only relevant principle is IV (no hardcoded credentials/endpoints), which the module satisfies via variables. This deviation is documented in the Complexity Tracking table.

**Rationale**: The constitution explicitly scopes Go constraints to `go.mod`-managed code.
