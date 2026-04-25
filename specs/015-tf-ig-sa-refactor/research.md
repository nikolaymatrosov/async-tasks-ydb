# Research: Per-IG Service Account Isolation

## Decision 1: IG Manager SA Role Set

**Decision**: Assign `compute.editor`, `iam.serviceAccounts.user`, `vpc.user`, `vpc.publicAdmin` to each IG manager SA.

**Rationale**: These are the exact roles the current shared `coi_vm` SA carries that are needed by the Instance Group control plane:
- `compute.editor` — create, update, delete, restart VMs inside the group
- `iam.serviceAccounts.user` — attach the VM SA to VMs defined in `instance_template`
- `vpc.user` — attach VMs to subnets
- `vpc.publicAdmin` — manage public IP addresses on VMs if needed

**Alternatives considered**: Granting `editor` at folder level would work but is over-privileged and violates least-privilege. A custom role would be ideal long-term but is not available in the Yandex Cloud Terraform provider without manual portal steps.

---

## Decision 2: VM SA Role Set

**Decision**: Assign `container-registry.images.puller`, `ydb.editor`, `monitoring.editor` to each VM SA.

**Rationale**: These are the application-level permissions needed by running VMs:
- `container-registry.images.puller` — Docker daemon pulls the COI image at boot
- `ydb.editor` — application reads and writes `coordinated_tasks` table
- `monitoring.editor` — Unified Agent pushes metrics to Yandex Monitoring

**Alternatives considered**: `ydb.admin` is over-privileged. `monitoring.viewer` is insufficient for writes. No other roles are required for the described workloads.

---

## Decision 3: Terraform Dependency Ordering for Safe Destroy

**Decision**: Declare `depends_on` on each `yandex_compute_instance_group` resource pointing to all IAM binding resources for both that IG's manager SA and VM SA.

**Rationale**: Terraform's `depends_on` encodes a directed edge A→B meaning "A depends on B". This causes:
- **Creation**: B is created before A
- **Destruction**: A is destroyed before B

So `yandex_compute_instance_group.depends_on = [iam_binding_1, iam_binding_2, ...]` means:
1. All IAM bindings exist before the IG is created (IG manager SA has roles before IG starts managing VMs)
2. The IG is destroyed before any IAM binding is removed (IG never loses permissions mid-lifecycle)

**Alternatives considered**:
- Putting `depends_on` on the IAM bindings pointing to the IG would invert the order — bindings destroyed first, IG left broken. This is the current bug.
- Using `lifecycle { prevent_destroy = true }` on role bindings prevents any destroy, not just premature ones. Too blunt.
- Terraform `create_before_destroy` is about replacement, not destroy ordering. Not applicable here.

---

## Decision 4: IAM Resource Co-location

**Decision**: Move all IAM resources (SA creation + role bindings) into each IG's own module (`producer/`, `workers/`). Create a new `iam.tf` file in each module.

**Rationale**: Each module should own its full IAM surface. This avoids the `db` module becoming a catch-all IAM hub. After the refactor, the `db` module retains only the bastion SA.

**Alternatives considered**: Centralizing all IAM in `db` module works but creates tight coupling — `db` module must know about producer and workers. A dedicated top-level `iam` module would add indirection without benefit given the simple structure.

---

## Decision 5: SA Naming Convention

**Decision**: Use names `async-tasks-producer-ig-sa`, `async-tasks-producer-vm-sa`, `async-tasks-workers-ig-sa`, `async-tasks-workers-vm-sa`.

**Rationale**: Names must be unique within the folder and self-describing. The pattern `<project>-<ig>-<role>-sa` encodes all necessary context.

**Alternatives considered**: Using random suffix (`${var.folder_id}-...`) adds uniqueness but hurts readability. Yandex Cloud SA names must be ≤ 63 chars, lowercase, alphanumeric and hyphens — all proposed names comply.

---

## Decision 6: Removal of `service_account_id` from db Module

**Decision**: Remove the `coi_vm` SA resource, its 7 IAM bindings, and the `service_account_id` output from the `db` module. Remove `service_account_id` input variable from `producer` and `workers` modules. Remove `service_account_id` from the module calls in `main.tf`.

**Rationale**: Once each IG module owns its SAs internally, there is no SA to export from `db`. Leaving the output would cause confusion about which SA is authoritative.

**Alternatives considered**: Keeping the shared SA as a fallback would preserve backwards compat but contradict the isolation goal and introduce ambiguity.
