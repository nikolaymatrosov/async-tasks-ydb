# Feature Specification: Bastion VM Migrations via COI + SSH Provisioner

**Feature Branch**: `012-bastion-vm-migrations`
**Created**: 2026-04-24
**Status**: Draft
**Input**: User description: "run migrations from bastion VM. Add service account to VM so migration tool can use its token as auth. Change bastion image family from ubuntu to COI, use SSH Provisioner to run migrations"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run migrations on infrastructure apply (Priority: P1)

An operator runs `terraform apply` and the bastion VM automatically executes the YDB migration tool over SSH. The migration tool authenticates to YDB using the VM's attached service account IAM token — no credentials or keys need to be passed manually.

**Why this priority**: This is the core requirement. All other stories depend on the bastion being correctly provisioned first.

**Independent Test**: After `terraform apply` completes successfully, query the YDB schema to confirm migration-created tables/changes exist without any manual operator steps.

**Acceptance Scenarios**:

1. **Given** a fresh `terraform apply` with no prior migration state, **When** Terraform finishes, **Then** the YDB database reflects all migration changes and the SSH provisioner exit code is 0.
2. **Given** migrations have already been applied, **When** `terraform apply` runs again with no infrastructure changes, **Then** the migration tool detects the existing state and exits cleanly (idempotent).
3. **Given** a migration failure (e.g., malformed schema), **When** the SSH provisioner runs, **Then** Terraform reports a non-zero exit and the apply fails, leaving the operator with a clear error message.

---

### User Story 2 - Bastion uses COI image (Priority: P2)

The bastion VM boots from a Container-Optimized Image instead of Ubuntu, removing the need to install Docker or any tooling manually.

**Why this priority**: Prerequisite for running containerised migration tooling; reduces image maintenance burden.

**Independent Test**: SSH into the bastion and confirm the OS is COI (not Ubuntu) and Docker is available without installation.

**Acceptance Scenarios**:

1. **Given** the Terraform config is applied, **When** the bastion VM boots, **Then** `uname -a` shows the COI kernel and `docker version` succeeds.
2. **Given** the old `data.yandex_compute_image.ubuntu` block existed, **When** the plan is generated, **Then** no reference to the `ubuntu-2404-lts` family remains.

---

### User Story 3 - Service account attached to bastion (Priority: P3)

The bastion VM has a dedicated service account with `ydb.editor` permission attached. The migration tool can retrieve an IAM token from the metadata service without any static credentials.

**Why this priority**: Without the service account, the migration tool cannot authenticate to YDB; the SSH provisioner in P1 depends on this.

**Independent Test**: SSH into the bastion, run `curl -H Metadata-Flavor:Google http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token` and receive a valid IAM token JSON.

**Acceptance Scenarios**:

1. **Given** the bastion VM is running, **When** the migration tool requests a token from the metadata service, **Then** a valid IAM token is returned.
2. **Given** the service account has `ydb.editor` role, **When** the migration tool uses the token to connect to YDB, **Then** schema operations succeed.
3. **Given** the service account is scoped to the bastion only (not shared with worker VMs), **When** the bastion is destroyed, **Then** worker IAM permissions are unaffected.

---

### Edge Cases

- What happens when the bastion public IP changes between applies? The SSH provisioner connection must resolve the current NAT IP from state.
- What happens if the YDB endpoint is not reachable from the bastion subnet at provisioner run time? Terraform should surface the error and leave infrastructure for debugging.
- What if the migration tool binary is not present on the COI image? The provisioner must pull or copy the binary before invoking it.
- What happens on partial migration (migration tool crashes mid-run)? The migration must be re-runnable safely (idempotent by design of the tool).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The bastion VM MUST use the `container-optimized-image` family from Yandex Compute instead of `ubuntu-2404-lts`.
- **FR-002**: The bastion VM MUST have a dedicated service account attached with at minimum `ydb.editor` role in the folder.
- **FR-003**: Terraform MUST use an SSH provisioner on the bastion compute resource to execute the migration tool after the VM is created.
- **FR-004**: The SSH provisioner MUST connect using the existing SSH key already configured for the bastion (no new key material introduced).
- **FR-005**: The migration tool MUST authenticate to YDB exclusively via IAM token retrieved from the VM metadata service — no static API keys or SA key files.
- **FR-006**: The provisioner execution MUST be idempotent: re-running `terraform apply` when the VM already exists MUST NOT re-run migrations unless the VM is recreated.
- **FR-007**: The Terraform plan MUST remove the `data.yandex_compute_image.ubuntu` data source and all references to it.
- **FR-008**: The bastion service account MUST be a separate resource from the `coi_vm` service account already used by worker/producer VMs.

### Key Entities

- **Bastion VM** (`yandex_compute_instance.bastion`): The jump-host that runs migrations; switches from Ubuntu to COI image; gains a service account binding.
- **Bastion Service Account** (`yandex_iam_service_account.bastion`): New SA attached to the bastion; holds `ydb.editor` role; distinct from the existing `coi_vm` SA.
- **SSH Provisioner**: Terraform `provisioner "remote-exec"` block on the bastion resource; runs the migration command on first VM creation.
- **Migration Tool**: The containerised or binary tool that applies YDB schema changes; invoked by the provisioner; uses metadata-service IAM token for auth.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `terraform apply` completes in a single run with zero manual operator steps beyond providing `terraform.tfvars`.
- **SC-002**: After apply, the YDB database contains all expected schema objects (tables, indexes) created by the migration tool — verifiable by querying the database.
- **SC-003**: The bastion VM plan contains no reference to the Ubuntu image family; the COI image data source is the sole image reference.
- **SC-004**: Re-running `terraform apply` with no infrastructure changes takes under 30 seconds and does not re-trigger SSH provisioner execution.
- **SC-005**: The migration tool's IAM token request to the metadata endpoint succeeds within 2 seconds of VM boot.

## Assumptions

- The existing `ssh_public_key` variable and the corresponding private key accessible to the Terraform executor are sufficient for SSH provisioner connectivity.
- The migration binary/container image is already built and available (either pre-loaded on COI or pulled at runtime); this feature does not cover building the migration tool.
- The COI family name in Yandex Cloud is `container-optimized-image` (standard family in the `standard-images` folder).
- The bastion already has a static external IP (`nat_ip_address = "111.88.240.80"`) which the SSH provisioner will use as the connection target.
- The private SSH key path will be supplied as a new Terraform variable (e.g., `ssh_private_key_path`) since provisioners need the private key, not just the public key.
- The existing `coi_vm` service account is **not** modified; a new `bastion` service account is created to keep roles minimal and auditable.
