---

description: "Task list for Terraform API Gateway Module"
---

# Tasks: Terraform API Gateway Module

**Input**: Design documents from `specs/011-apigw-terraform-module/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Exact file paths are included in all task descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the new module directory structure

- [X] T001 Create directory `terraform/modules/apigw/` (four empty placeholder files: versions.tf, variables.tf, main.tf, outputs.tf)

---

## Phase 2: Foundational (Module Files — Blocking Prerequisites)

**Purpose**: Create the four reusable module files; root integration in Phase 3 depends on all four files existing

**⚠️ CRITICAL**: Phase 3 cannot begin until all four module files are complete

- [X] T002 [P] Create `terraform/modules/apigw/versions.tf` with `required_providers { yandex = { source = "yandex-cloud/yandex" } }` block
- [X] T003 [P] Create `terraform/modules/apigw/variables.tf` with five variables: `name` (string, required), `description` (string, default ""), `folder_id` (string, required), `spec` (string, required), `labels` (map(string), default {}) — each with a description
- [X] T004 [P] Create `terraform/modules/apigw/main.tf` with a single `resource "yandex_api_gateway" "main"` block wiring all five variables
- [X] T005 [P] Create `terraform/modules/apigw/outputs.tf` with two outputs: `gateway_id` (value = `yandex_api_gateway.main.id`) and `gateway_domain` (value = `yandex_api_gateway.main.domain`)

**Checkpoint**: Module is self-contained and syntactically valid — root integration can now begin

---

## Phase 3: User Story 1 — Deploy API Gateway via Terraform (Priority: P1) 🎯 MVP

**Goal**: A developer can invoke the `apigw` module from the root Terraform config, run `terraform plan`, and see a `yandex_api_gateway` resource in the plan output with the expected attributes.

**Independent Test**: Run `terraform validate` in `terraform/` and then `terraform plan`; confirm the plan contains a `yandex_api_gateway` resource with correct name, description, and spec, and that `gateway_id` / `gateway_domain` appear in the planned outputs.

### Implementation for User Story 1

- [X] T006 [P] [US1] Append three new variables to `terraform/variables.tf`: `apigw_name` (string, default "async-tasks-apigw"), `apigw_description` (string, default ""), `apigw_spec_file` (string, default "apigw-spec.yaml") — each with a description
- [X] T007 [P] [US1] Create `terraform/apigw-spec.yaml` with minimal valid OpenAPI 3.0 stub: `openapi: "3.0.0"`, `info.title: async-tasks API`, `info.version: "1.0"`, `x-yc-apigateway.service_account_id: ""`, `paths: {}`
- [X] T008 [US1] Append `module "apigw"` block to `terraform/main.tf`: `source = "./modules/apigw"`, `name = var.apigw_name`, `description = var.apigw_description`, `folder_id = var.folder_id`, `spec = file("${path.module}/${var.apigw_spec_file}")` (depends on T006, T007)
- [X] T009 [US1] Append two new outputs to `terraform/outputs.tf`: `gateway_id` (value = `module.apigw.gateway_id`) and `gateway_domain` (value = `module.apigw.gateway_domain`) (depends on T008)
- [X] T010 [US1] Run `terraform validate` inside `terraform/` and resolve any syntax or schema errors in the new files

**Checkpoint**: `terraform validate` passes; `terraform plan` shows `yandex_api_gateway.main` resource and two new root outputs — User Story 1 is independently verifiable

---

## Phase 4: User Story 2 — Integrate API Gateway with Existing Infra Modules (Priority: P2)

**Goal**: The `apigw` module block in `terraform/main.tf` coexists with `db`, `workers`, and `producer` module blocks without circular dependencies or missing-reference errors.

**Independent Test**: Run `terraform plan` on the full root config (all modules present); confirm no dependency graph errors and that `module.apigw.gateway_domain` can be referenced in a local value or output without error.

### Implementation for User Story 2

- [X] T011 [US2] Review `terraform/main.tf` and confirm the `module "apigw"` block has no `depends_on` referencing other modules; it receives only `var.folder_id` (shared variable, no cross-module output dependency) — add a comment if the independence needs to be explicit
- [X] T012 [US2] Verify `terraform/outputs.tf` exposes `gateway_id` and `gateway_domain` at root level so downstream modules or CI pipelines can consume them without reading module internals

**Checkpoint**: `terraform plan` with all modules resolves cleanly; no missing-reference or circular-dependency errors

---

## Phase 5: User Story 3 — Destroy API Gateway Cleanly (Priority: P3)

**Goal**: Running `terraform destroy` removes the `yandex_api_gateway` resource before any dependent resources are touched, with no orphaned state.

**Independent Test**: Review `terraform/modules/apigw/main.tf` to confirm no `lifecycle` blocks that could prevent or delay destruction; confirm `terraform plan -destroy` lists the resource for deletion without errors.

### Implementation for User Story 3

- [X] T013 [US3] Confirm `terraform/modules/apigw/main.tf` has no `lifecycle { prevent_destroy = true }` or `create_before_destroy` that would complicate teardown; if absent, this task is a no-op review
- [X] T014 [US3] Run `terraform plan -destroy` (or inspect the destroy plan output) and confirm the `yandex_api_gateway` resource appears in the destroy list; document the expected destroy order in a comment in `terraform/modules/apigw/main.tf` only if the order is non-obvious

**Checkpoint**: Destroy plan lists the gateway resource; no lifecycle blocks block clean removal

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final formatting, linting, and validation passes across all new and modified files

- [X] T015 [P] Run `terraform fmt -recursive terraform/` and commit any whitespace/indentation fixes across all new and modified HCL files
- [X] T016 Run `terraform validate` one final time after formatting to confirm the full module tree is valid with no warnings about missing descriptions or deprecated syntax

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 — BLOCKS all user story phases
- **User Story 1 (Phase 3)**: Depends on Phase 2 (all four module files must exist)
- **User Story 2 (Phase 4)**: Depends on Phase 3 completion (module "apigw" block must exist in root)
- **User Story 3 (Phase 5)**: Depends on Phase 3 completion (module must be wired in)
- **Polish (Phase 6)**: Depends on all story phases completing

### User Story Dependencies

- **US1 (P1)**: Depends on Foundational — no dependency on US2 or US3
- **US2 (P2)**: Depends on US1 (needs the module block to validate integration) — does not modify files, only verifies
- **US3 (P3)**: Depends on US1 (needs the module wired) — does not modify files, only verifies

### Within Phase 3 (US1)

- T006 and T007 are independent and can run in parallel
- T008 depends on T006 and T007 completing
- T009 depends on T008 completing
- T010 (validate) runs after T009

### Parallel Opportunities

- Phase 2 tasks T002–T005: all target different files, run in parallel
- T006 and T007: different files, run in parallel
- Phase 6 tasks T015–T016: T015 first, then T016

---

## Parallel Example: Phase 2

```bash
# All four module files can be created simultaneously:
Task T002: "Create terraform/modules/apigw/versions.tf"
Task T003: "Create terraform/modules/apigw/variables.tf"
Task T004: "Create terraform/modules/apigw/main.tf"
Task T005: "Create terraform/modules/apigw/outputs.tf"
```

## Parallel Example: User Story 1 (Phase 3)

```bash
# T006 and T007 run in parallel:
Task T006: "Append three new variables to terraform/variables.tf"
Task T007: "Create terraform/apigw-spec.yaml placeholder"

# Then sequentially:
Task T008: "Append module "apigw" block to terraform/main.tf"
Task T009: "Append gateway_id and gateway_domain outputs to terraform/outputs.tf"
Task T010: "Run terraform validate"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (create directory)
2. Complete Phase 2: Foundational (4 module files)
3. Complete Phase 3: User Story 1 (root wiring + placeholder spec)
4. **STOP and VALIDATE**: Run `terraform validate` + `terraform plan`
5. Deploy/demo if ready

### Incremental Delivery

1. Phase 1 + Phase 2 → Module is self-contained and reusable
2. Phase 3 → Root integration works, `terraform plan` shows gateway resource → **MVP**
3. Phase 4 → Integration with existing modules verified
4. Phase 5 → Clean destroy confirmed
5. Phase 6 → All files formatted, no `terraform validate` warnings

---

## Notes

- No tests were explicitly requested in the feature spec; no test tasks are included
- [P] tasks target different files and have no blocking dependencies on each other
- US2 and US3 are primarily verification tasks; the implementation is complete after US1
- `terraform validate` is the primary correctness gate — run it after every phase
- No new providers are added; `yandex-cloud/yandex` is already declared in the root `terraform/main.tf`
