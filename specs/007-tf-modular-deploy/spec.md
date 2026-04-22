# Feature Specification: Terraform Modular Deployment

**Feature Branch**: `007-tf-modular-deploy`  
**Created**: 2026-04-22  
**Status**: Draft  
**Input**: User description: "add producer ig. Organize everything in tf folder to modules. So I can deploy Db, workers and producer separately by specifing the target"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Deploy Database Independently (Priority: P1)

An operator needs to provision only the YDB database and its supporting network/IAM resources without touching the worker or producer deployments. This is the foundation all other components depend on, so it must be deployable in isolation first.

**Why this priority**: The database is the shared dependency for all other components. Being able to deploy it standalone is required for initial setup and for database-only changes (scaling, storage config).

**Independent Test**: Can be fully tested by running a database-targeted deploy and verifying the YDB endpoint is reachable, with no compute instances created.

**Acceptance Scenarios**:

1. **Given** a fresh cloud environment, **When** the operator deploys only the database module, **Then** a YDB instance, VPC network, subnets, container registry, and the required IAM service account are created — and no compute instance groups are created.
2. **Given** an existing database deployment, **When** the operator re-applies the database module, **Then** only database-related resources are updated and worker/producer resources are unaffected.
3. **Given** the database module output values (endpoint, database path), **When** the workers or producer module is deployed separately, **Then** those modules can consume the database outputs as inputs without re-deploying the database.

---

### User Story 2 - Deploy Workers Independently (Priority: P2)

An operator wants to scale or reconfigure the worker instance group without touching the database or producer. Workers run the CDC/coordinated-table consumer logic and should be manageable as a standalone unit.

**Why this priority**: Worker configuration changes (instance count, image version, VM size) are the most frequent operational action. Isolating them prevents accidental database re-creation.

**Independent Test**: Can be fully tested by deploying the workers module against an already-deployed database, verifying a compute instance group is created/updated and workers connect to the YDB endpoint.

**Acceptance Scenarios**:

1. **Given** the database module is already deployed, **When** the operator deploys only the workers module, **Then** a compute instance group running the worker container image is created and connects to the YDB endpoint.
2. **Given** a running workers deployment, **When** the operator changes the worker image version and re-applies only the workers module, **Then** the instance group is updated and the database is not modified.
3. **Given** an operator who destroys only the workers module, **When** the destroy completes, **Then** the database and producer remain intact.

---

### User Story 3 - Deploy Producer Independently (Priority: P2)

An operator wants to deploy or update the producer service (db-producer) without affecting the database or workers. The producer writes tasks into YDB and should be deployable as its own compute unit.

**Why this priority**: The producer is a new component being added. Independent deployability gives operators full control over task ingestion without risk to existing infrastructure.

**Independent Test**: Can be fully tested by deploying the producer module against an already-deployed database, verifying the producer container is running and successfully writing tasks to YDB.

**Acceptance Scenarios**:

1. **Given** the database module is already deployed, **When** the operator deploys only the producer module, **Then** a compute instance (or instance group) running the db-producer container image is created and connects to the YDB endpoint.
2. **Given** a running producer deployment, **When** the operator updates the producer image version and re-applies only the producer module, **Then** the producer is updated and workers/database are unaffected.
3. **Given** an operator who destroys only the producer module, **When** the destroy completes, **Then** the database and workers remain intact.

---

### User Story 4 - Deploy Full Stack at Once (Priority: P3)

An operator wants to bring up the entire infrastructure (database + workers + producer) in a single command for a fresh environment, without specifying individual targets.

**Why this priority**: A single-command full deploy is valuable for CI/CD pipelines and new environment bootstrapping, but it is less critical than per-component control.

**Independent Test**: Can be tested by running an unconstrained `apply` from the root module and verifying all three component outputs are present and healthy.

**Acceptance Scenarios**:

1. **Given** an empty cloud folder, **When** the operator runs a full apply with no module target, **Then** the database, workers, and producer are all created in dependency order (database first, then workers and producer).
2. **Given** a fully deployed stack, **When** the operator runs a full apply with no changes, **Then** the plan shows no changes across all modules.

---

### Edge Cases

- What happens when the database module has not been deployed yet and an operator tries to deploy only the workers or producer module? The operator should receive a clear error indicating the database outputs are required.
- How does the system handle a partial deployment failure mid-apply (e.g., database created but workers failed)? Each module should be independently re-appliable so the operator can retry just the failed module.
- What happens when the producer module is destroyed but workers are still running? Workers should continue operating against the database unaffected.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The Terraform configuration MUST be reorganised into at least three independently deployable modules: `db` (database, network, registry, IAM), `workers` (worker instance group), and `producer` (producer instance or instance group).
- **FR-002**: Each module MUST be deployable in isolation using a standard Terraform module-targeting mechanism, without causing unintended changes to other modules.
- **FR-003**: The `db` module MUST expose its outputs (YDB endpoint, database path, registry ID, service account ID, subnet IDs) so that `workers` and `producer` modules can consume them as inputs.
- **FR-004**: The `producer` module MUST deploy the db-producer container image as a running compute workload connected to the YDB database.
- **FR-005**: The root Terraform configuration MUST wire all three modules together so that a full apply (no target) deploys the complete stack in correct dependency order.
- **FR-006**: Container image build and push for the producer MUST be part of the producer module's lifecycle, consistent with how the workers module handles its images.
- **FR-007**: All existing variables and outputs MUST remain accessible; no currently-supported deployment workflow should be broken by the reorganisation.
- **FR-008**: The IAM service account and its role bindings MUST be managed in the `db` module (or a shared foundation module) and referenced by both workers and producer modules, avoiding duplication.

### Key Entities

- **`db` module**: Provisions the YDB database, VPC network and subnets, container registry, IAM service account, and all IAM role bindings. Exports outputs consumed by downstream modules.
- **`workers` module**: Provisions the worker compute instance group. Consumes `db` module outputs. Manages the worker container image build/push lifecycle.
- **`producer` module**: Provisions the producer compute workload. Consumes `db` module outputs. Manages the producer container image build/push lifecycle.
- **Root module**: Composes `db`, `workers`, and `producer` modules. Aggregates outputs. Accepts the full variable set.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can deploy only the database by specifying a single target, and the operation completes with zero compute instances created.
- **SC-002**: An operator can deploy only the workers by specifying a single target against an existing database, with no database resources modified.
- **SC-003**: An operator can deploy only the producer by specifying a single target against an existing database, with no database or worker resources modified.
- **SC-004**: A full apply with no target provisions all three components in under 15 minutes on a fresh cloud folder.
- **SC-005**: All previously supported outputs (YDB endpoint, database path, registry ID, instance group ID) remain available after the reorganisation.
- **SC-006**: No existing variables are removed or renamed in a breaking way; existing `terraform.tfvars` files continue to work without modification.

## Assumptions

- The producer runs as a Yandex Compute instance group (consistent with the workers pattern), not as a standalone VM or serverless container.
- A single IAM service account is shared between workers and producer; it is owned by the `db` (or foundation) module to avoid circular dependencies.
- The `migrations` resource remains in the `db` module since it depends on the YDB endpoint.
- The container registry is part of the `db` module because it is a shared resource referenced by both workers and producer image builds.
- Terraform module outputs are passed explicitly as input variables to downstream modules (no data source lookups across modules).
- The existing `terraform/` directory is reorganised in-place; the new module structure replaces the flat file layout.
