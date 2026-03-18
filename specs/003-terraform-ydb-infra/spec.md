# Feature Specification: Terraform Infrastructure for YDB Cluster and Container-Optimized VMs

**Feature Branch**: `003-terraform-ydb-infra`
**Created**: 2026-03-17
**Status**: Draft
**Input**: User description: "I want you to add terraform that creates YDB cluster, packs all examples into distroless container and creates VM from family = container-optimized-image"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Provision Full Infrastructure (Priority: P1)

A developer wants to deploy the complete infrastructure — YDB cluster, container images for all examples, and a container-optimized VM — using a single declarative command. This allows anyone to reproduce the environment from scratch without manual steps.

**Why this priority**: Without the infrastructure being provisionable, none of the other stories are possible. This is the foundational delivery.

**Independent Test**: Can be tested by running the provisioning command in a clean Yandex Cloud folder and verifying all resources appear in the cloud console.

**Acceptance Scenarios**:

1. **Given** a configured Yandex Cloud folder with appropriate permissions, **When** the infrastructure provisioning command is run, **Then** a YDB cluster, container registry, and a VM are all created successfully.
2. **Given** previously provisioned infrastructure, **When** the teardown command is run, **Then** all created resources are removed with no orphaned resources remaining.

---

### User Story 2 - Run Example Applications on Container-Optimized VM (Priority: P2)

A developer wants to run the repository's example applications as distroless containers on a dedicated VM that is purpose-built for running containers. This provides a minimal, secure runtime environment for the examples.

**Why this priority**: The VM and containers are the primary runtime for the examples; they depend on the infrastructure from Story 1.

**Independent Test**: Can be tested by SSHing into the provisioned VM and verifying that all example containers are running and responsive.

**Acceptance Scenarios**:

1. **Given** a provisioned VM, **When** the VM starts up, **Then** all example container images are pulled and running automatically.
2. **Given** a running container, **When** the application inside the container is queried, **Then** it responds correctly without requiring a full OS environment.
3. **Given** a newly built image from the repository source, **When** provisioning runs, **Then** the latest example binaries are packaged and deployed.

---

### User Story 3 - Examples Connect to YDB Cluster (Priority: P3)

A developer wants the example applications running on the VM to connect to and operate against the provisioned YDB cluster, demonstrating end-to-end functionality of the system.

**Why this priority**: This validates the integration between all infrastructure components. It depends on Stories 1 and 2.

**Independent Test**: Can be tested by inspecting the example application logs on the VM for successful database operations.

**Acceptance Scenarios**:

1. **Given** a running example container and a provisioned YDB cluster, **When** the example application starts, **Then** it successfully authenticates and connects to the YDB cluster.
2. **Given** a connected example application, **When** it performs database read/write operations, **Then** the operations complete successfully.

---

### Edge Cases

- What happens when the Yandex Cloud folder lacks required quotas (CPU, disk, database) for all resources?
- How does provisioning handle partial failures — e.g., YDB cluster created but VM creation fails?
- What happens when a container image build fails due to a source code compilation error?
- How does the VM behave when the container registry is temporarily unavailable during startup?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Infrastructure MUST provision a managed YDB database cluster in the specified Yandex Cloud folder.
- **FR-002**: All example applications in the repository MUST be compiled and packaged as distroless container images.
- **FR-003**: Container images MUST be pushed to a container registry accessible by the VM.
- **FR-004**: A compute instance MUST be created using the container-optimized OS image family.
- **FR-005**: The compute instance MUST automatically pull and run the example container images on startup.
- **FR-006**: The VM's service account MUST have permissions to pull images from the container registry and connect to the YDB cluster.
- **FR-007**: The YDB cluster MUST be network-accessible from the compute instance.
- **FR-008**: All infrastructure resources MUST be defined as declarative configuration files that can be applied idempotently.
- **FR-009**: Infrastructure MUST be destroyable in a single command, removing all provisioned resources cleanly.
- **FR-010**: Variable inputs (folder ID, cloud ID, zone, etc.) MUST be configurable without modifying core configuration files.

### Key Entities

- **YDB Cluster**: Managed database service instance that stores and processes task data; the backend for all example applications.
- **Container Image**: A compiled example application packaged in a minimal distroless base, stored in the container registry.
- **Container Registry**: Private image storage that holds all built example images; accessible only by the VM's service account.
- **Compute Instance**: A VM running a container-optimized OS that pulls and executes example containers on startup.
- **Service Account**: The cloud identity assigned to the VM, granting it permissions to access the registry and YDB cluster.
- **VPC Network / Subnet**: The private network providing connectivity between the VM and the YDB cluster.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Full infrastructure (YDB cluster, container registry, VM) provisions successfully from zero with a single command and completes without errors.
- **SC-002**: All example applications are available as running containers on the VM within 5 minutes of VM boot.
- **SC-003**: Each example application successfully connects to the YDB cluster and completes at least one read and one write operation without errors.
- **SC-004**: Infrastructure teardown removes 100% of provisioned resources with no manual cleanup required.
- **SC-005**: Container images are built from the distroless base, resulting in images with no unnecessary OS components.
- **SC-006**: A developer with no prior knowledge of the deployment can provision the full environment by following a README and providing only cloud credentials and folder/cloud IDs.

## Assumptions

- The target cloud platform is Yandex Cloud; all resources are provisioned within a single Yandex Cloud folder.
- The repository contains multiple example applications (e.g., `src/` directory) that each compile to a standalone binary.
- Each example application will be packaged as a separate container image with a distroless base image.
- The container-optimized VM will use Docker or a compatible container runtime pre-installed in the OS image.
- YDB cluster authentication uses Yandex Cloud service account credentials (IAM token / metadata service), not static credentials.
- A container registry in the same Yandex Cloud folder will be created as part of this feature.
- Network connectivity between the VM and YDB cluster is achieved via private VPC networking within the same availability zone.
- The VM is single-instance (no auto-scaling group) for the purposes of this feature.
- Terraform state will be managed locally by default; remote state backend is out of scope.

## Dependencies & Constraints

- Yandex Cloud account with sufficient quotas for: 1 YDB cluster, 1 VM instance, 1 container registry.
- Yandex Cloud credentials (OAuth token or service account key) must be available in the local environment before provisioning.
- All example applications must build successfully before container images can be created.
- The container-optimized image family must support the container runtime required by the examples.
