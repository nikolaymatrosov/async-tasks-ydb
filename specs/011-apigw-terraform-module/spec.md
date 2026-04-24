# Feature Specification: Terraform API Gateway Module

**Feature Branch**: `011-apigw-terraform-module`
**Created**: 2026-04-24
**Status**: Draft
**Input**: User description: "add terraform module with API gateway resource"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Deploy API Gateway via Terraform (Priority: P1)

A developer managing the async-tasks infrastructure wants to provision a Yandex Cloud API Gateway resource using a reusable Terraform module alongside the existing YDB and compute resources. They run `terraform apply` and an API Gateway is created with the desired configuration, without having to write raw provider resource blocks directly in the root module.

**Why this priority**: The entire feature is the module itself — exposing the API Gateway resource as a composable unit is the primary deliverable.

**Independent Test**: Can be tested by invoking the module from a root Terraform configuration, running `terraform plan`, and verifying a `yandex_api_gateway` resource appears in the planned output with the expected attributes.

**Acceptance Scenarios**:

1. **Given** a root Terraform configuration that calls the `apigw` module with required inputs, **When** `terraform plan` is run, **Then** the plan includes a `yandex_api_gateway` resource with the provided name, description, and OpenAPI spec.
2. **Given** a valid module invocation, **When** `terraform apply` succeeds, **Then** the module outputs the gateway ID and default domain so callers can reference them in other resources.
3. **Given** a module invocation with an invalid OpenAPI spec path, **When** `terraform plan` is run, **Then** Terraform surfaces a clear validation error before any infrastructure is created.

---

### User Story 2 - Integrate API Gateway with Existing Infra Modules (Priority: P2)

A developer wires the new `apigw` module into the existing modular Terraform layout (established in feature 007) so the gateway endpoint is automatically linked to the YDB backend and worker compute resources.

**Why this priority**: The module is most valuable when it composes with existing modules; standalone use is secondary to integration.

**Independent Test**: Can be tested by referencing the module's output (`gateway_domain`) from another module's input and confirming `terraform plan` resolves the dependency without errors.

**Acceptance Scenarios**:

1. **Given** the `apigw` module is declared in the same root config as `ydb` and `worker` modules, **When** `terraform plan` is run, **Then** no circular dependencies or missing-reference errors appear.
2. **Given** the gateway module outputs `gateway_id` and `gateway_domain`, **When** another module consumes those outputs, **Then** `terraform apply` wires the values correctly without manual intervention.

---

### User Story 3 - Destroy API Gateway Cleanly (Priority: P3)

A developer tears down the environment. The API Gateway is removed in the correct dependency order without leaving orphaned resources.

**Why this priority**: Clean destruction is required for cost control and CI/CD environments but is lower priority than provisioning.

**Independent Test**: Can be tested by running `terraform destroy` and confirming the `yandex_api_gateway` resource is removed with exit code 0.

**Acceptance Scenarios**:

1. **Given** an applied stack with the `apigw` module, **When** `terraform destroy` is run, **Then** the API Gateway resource is deleted before dependent resources are touched.
2. **Given** an API Gateway that is in use, **When** destroy is attempted, **Then** Terraform reports a human-readable error instead of leaving the state in an inconsistent condition.

---

### Edge Cases

- What happens when the OpenAPI spec file referenced by the module does not exist at plan time?
- How does the module behave when the Yandex Cloud folder ID is missing or incorrect?
- What happens when an API Gateway with the same name already exists outside Terraform state (import scenario)?
- How are gateway configuration updates (spec changes) handled — in-place update vs. replacement?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The module MUST accept the API Gateway name, description, folder ID, and an OpenAPI specification (inline string or file path) as inputs.
- **FR-002**: The module MUST create a `yandex_api_gateway` resource using the provided inputs.
- **FR-003**: The module MUST expose `gateway_id` and `gateway_domain` as outputs for use by other modules.
- **FR-004**: The module MUST be callable from the project's existing modular Terraform root without requiring new provider configurations.
- **FR-005**: The module MUST be placed under the existing `terraform/modules/` directory structure consistent with feature 007 conventions.
- **FR-006**: All module inputs MUST have descriptions and types; optional inputs MUST have defaults so the module can be invoked with minimal required arguments.
- **FR-007**: The module MUST NOT hardcode folder IDs, zone names, or credentials — all environment-specific values MUST be passed in as variables.

### Key Entities

- **API Gateway**: A Yandex Cloud managed API Gateway resource defined by an OpenAPI 3.0 spec. Key attributes: name, description, folder ID, spec content/path, gateway ID (output), default domain (output).
- **Module**: A reusable Terraform module encapsulating the API Gateway resource, its variables, and outputs.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer can add the API Gateway to a new environment by writing fewer than 15 lines of Terraform in the root module (module invocation + variable assignments).
- **SC-002**: `terraform plan` completes without errors when the module is invoked with all required inputs provided.
- **SC-003**: `terraform apply` creates the API Gateway resource and the module outputs are populated within the same apply run.
- **SC-004**: The module can be destroyed and re-created in a fresh environment without any manual state manipulation.
- **SC-005**: No Terraform warnings about deprecated syntax or missing descriptions appear when running `terraform validate` against the module.

## Assumptions

- The Yandex Cloud Terraform provider (`yandex-cloud/yandex`) is already configured in the project's provider block; the module does not add or re-declare providers.
- The project uses sequential spec/branch numbering (confirmed: this is feature 011).
- An OpenAPI 3.0 spec for the API Gateway already exists or will be authored separately; this feature delivers the Terraform module wrapper, not the API spec content.
- The existing `terraform/modules/` directory (from feature 007) is the canonical location for new modules.
- The dummy API gateway configuration explored in feature 010 (`010-yc-apigw-dummy`) serves as the reference implementation to be extracted into the reusable module.
