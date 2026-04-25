# Feature Specification: Per-IG Service Account Isolation with Safe Dependency Ordering

**Feature Branch**: `015-tf-ig-sa-refactor`
**Created**: 2026-04-25
**Status**: Draft
**Input**: User description: "refactor terraform. Each IG should have its own Service Account for managing VM and separate SA for VM themselves. Proper set of roles should be assigned to the SA's. Those roles should depend on IG so Terraform won't delete them before IG is deleted, leaving IG in disfunctional state when it is unable to manage underlying VM."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Producer IG Gets Isolated Service Accounts (Priority: P1)

As a platform operator, when the producer Instance Group is provisioned, it must use a dedicated SA for IG management operations and a separate dedicated SA for the producer VMs themselves. The producer IG manager SA must have only the roles needed to manage the IG lifecycle. The producer VM SA must have only the roles needed by the running VMs (pull images, write to YDB, write metrics).

**Why this priority**: The producer IG is always provisioned. Isolating its SAs is the foundational change—workers depend on the same pattern.

**Independent Test**: Apply Terraform targeting only the producer module. Verify that two distinct SAs exist for the producer IG (one manager SA, one VM SA). Verify that the IG resource references the manager SA, and the instance template references the VM SA. Verify the producer VMs can pull container images, connect to YDB, and emit metrics.

**Acceptance Scenarios**:

1. **Given** no prior deployment, **When** `terraform apply` is run, **Then** two SAs named after the producer IG are created — one for IG management and one for VM identity.
2. **Given** the producer IG exists, **When** inspecting the `yandex_compute_instance_group` resource, **Then** `service_account_id` (IG level) points to the IG manager SA and `instance_template.service_account_id` points to the VM SA.
3. **Given** the producer IG is destroyed, **When** `terraform destroy` targets the producer module, **Then** IAM role bindings for both SAs are removed only after the IG resource is fully deleted.
4. **Given** the producer VMs are running, **When** they attempt to pull a container image, access YDB, and write monitoring metrics, **Then** all operations succeed using the VM SA credentials.

---

### User Story 2 - Workers IG Gets Isolated Service Accounts (Priority: P1)

As a platform operator, when the workers Instance Group is provisioned (with autoscaling), it must use a dedicated SA for IG management and a separate SA for worker VMs. The workers IG manager SA must have only the roles necessary for autoscale operations. The workers VM SA must have only the roles required by the running worker processes.

**Why this priority**: Workers use autoscale, which has stricter IG management requirements. Isolation prevents over-privileged shared SAs from being a risk vector.

**Independent Test**: Apply Terraform targeting only the workers module. Verify two distinct SAs exist for the workers IG. Autoscale events (scale-up, scale-down) complete without IAM errors.

**Acceptance Scenarios**:

1. **Given** no prior deployment, **When** `terraform apply` is run for the workers module, **Then** two distinct SAs are created: one for IG management, one for worker VM identity.
2. **Given** the workers IG is in autoscale mode, **When** CPU utilization crosses the threshold, **Then** the IG manager SA successfully provisions or removes VMs without IAM errors.
3. **Given** the workers IG is destroyed, **When** `terraform destroy` runs, **Then** IAM role bindings are removed only after the IG is deleted — never before.
4. **Given** worker VMs are running, **When** they access YDB and emit metrics, **Then** all operations succeed using the worker VM SA.

---

### User Story 3 - Safe IAM Lifecycle During Destroy (Priority: P2)

As a platform operator, when running `terraform destroy` or `terraform apply` with removed IG resources, Terraform must destroy the IG before removing any IAM role bindings that the IG's management SA or VM SA depends on. The IG must never be left in a state where it cannot manage its underlying VMs.

**Why this priority**: The original problem statement — IG left dysfunctional if roles are removed first. This story validates the dependency ordering is correct.

**Independent Test**: Run `terraform destroy` on a deployed environment. Inspect the destroy plan and actual resource destruction order: IG resources must appear before (destroyed before) IAM binding resources in the sequence.

**Acceptance Scenarios**:

1. **Given** a fully deployed environment with both IGs, **When** `terraform destroy` is executed, **Then** both `yandex_compute_instance_group` resources are destroyed before any `yandex_resourcemanager_folder_iam_member` resources for their respective SAs.
2. **Given** the IG resource is removed from Terraform config, **When** `terraform plan` is run, **Then** the plan shows the IG is destroyed before the dependent IAM bindings are removed.
3. **Given** the IG manager SA roles are updated, **When** `terraform apply` runs, **Then** new roles are added before old roles are removed, with no gap where the IG has insufficient permissions.

---

### Edge Cases

- What happens when an IG module is removed entirely from root config while its IAM bindings still exist?
- How does the system behave when two IGs share a folder but have independent SAs — do role removals for one IG affect the other?
- What happens if the IG manager SA is accidentally deleted outside Terraform? (Terraform should detect drift and re-create.)
- What if `terraform apply` is interrupted mid-run after IG creation but before IAM bindings? (IG exists but lacks permissions — must be recoverable by re-running apply.)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Each Instance Group module (producer, workers) MUST create a dedicated SA for IG management operations, distinct from the VM SA.
- **FR-002**: Each Instance Group module MUST create a dedicated SA for VM instance identity, distinct from the IG management SA.
- **FR-003**: The IG management SA MUST be assigned only the roles required for Instance Group lifecycle management (create, update, delete VMs; autoscale for workers).
- **FR-004**: The VM SA MUST be assigned only the roles required by the running application processes (container image pull, YDB access, monitoring write).
- **FR-005**: IAM role binding resources for both SAs MUST declare an explicit Terraform dependency on the corresponding `yandex_compute_instance_group` resource, so that the IG is destroyed before its IAM bindings during `terraform destroy` or resource removal.
- **FR-006**: The `yandex_compute_instance_group` resource MUST declare an explicit Terraform dependency on the IAM role binding resources for its management SA, so that roles exist before the IG is created and are not removed while the IG exists.
- **FR-007**: SA and IAM resources MUST be co-located within each IG's module (producer or workers), not in the shared db module, so that each module owns its full IAM surface.
- **FR-008**: The shared `coi_vm` SA currently defined in the db module MUST be removed once per-IG SAs are in place; the db module's IAM responsibility is limited to the bastion SA.
- **FR-009**: SA names MUST be unique within the Yandex Cloud folder and MUST encode the IG they belong to (e.g., `async-tasks-producer-ig-sa`, `async-tasks-producer-vm-sa`, `async-tasks-workers-ig-sa`, `async-tasks-workers-vm-sa`).
- **FR-010**: The root module MUST NOT pass a shared `service_account_id` to the workers and producer modules; each module MUST derive its own SA IDs internally.

### Key Entities

- **IG Management SA**: A service account whose identity is used by the Instance Group control plane to provision and manage underlying VMs. One per IG.
- **VM SA**: A service account assigned to individual VM instances at boot time, used by application processes running inside the VM. One per IG.
- **IAM Role Binding**: A Terraform resource (`yandex_resourcemanager_folder_iam_member`) that grants a specific role to a SA within the folder. Must have explicit lifecycle dependencies.
- **Instance Group (IG)**: A `yandex_compute_instance_group` resource. References the IG management SA at the top level and the VM SA inside `instance_template`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After `terraform apply`, each IG has exactly two SAs — one manager SA and one VM SA — verified by listing folder SAs.
- **SC-002**: `terraform destroy` plan shows zero IAM binding resources scheduled for removal before their corresponding IG resource in the destroy sequence.
- **SC-003**: VMs in both IGs successfully complete their startup checks (image pull, YDB connection, metrics emission) within the same time window as before the refactor.
- **SC-004**: No IAM-related errors appear in IG audit logs or VM system logs after the refactor is applied.
- **SC-005**: The db module exports no `service_account_id` used by producer or workers modules after the refactor.
- **SC-006**: A full `terraform plan` after the refactor produces no unexpected diffs on stable infrastructure (idempotent).

## Assumptions

- The Yandex Cloud `yandex-cloud/yandex` Terraform provider version already in use supports `depends_on` on `yandex_resourcemanager_folder_iam_member` resources — no provider upgrade required.
- The required roles for IG management in Yandex Cloud are: `compute.editor`, `iam.serviceAccounts.user`, `vpc.user`, `vpc.publicAdmin`. (These are the roles currently assigned to the shared SA that are needed for IG to manage VMs.)
- The required roles for VM identity are: `container-registry.images.puller`, `ydb.editor`, `monitoring.editor`. (Application-level access only.)
- Terraform resource destruction order is controlled via `depends_on` on both sides of the IG ↔ IAM binding relationship; the Yandex provider does not provide a native lifecycle mechanism for this.
- The bastion SA (currently in db module) is out of scope for this refactor.
- No other modules outside producer and workers reference the shared `coi_vm` SA.
