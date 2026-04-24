# Research: Bastion VM Migrations (012)

## Decision 1: COI image family name

**Decision**: Use `family = "container-optimized-image"` in `data "yandex_compute_image" "coi"`.
**Rationale**: This is already the family name used by `terraform/modules/workers/compute.tf` and `terraform/modules/producer/compute.tf` — confirmed in repo. No research gap.
**Alternatives considered**: N/A — confirmed from existing codebase.

---

## Decision 2: Service account for bastion

**Decision**: Create a new `yandex_iam_service_account.bastion` with `ydb.editor` role in `terraform/modules/db/iam.tf`, export its ID as `bastion_service_account_id` from `modules/db/outputs.tf`, and bind it to the bastion compute instance via `service_account_id`.
**Rationale**: The existing `coi_vm` SA holds broad roles (registry puller, compute editor, vpc admin, IAM SA user) needed by worker/producer VMs. Giving the bastion its own minimal SA (`ydb.editor` only) limits blast radius if the bastion is compromised.
**Alternatives considered**: Reuse `coi_vm` SA — rejected because it would grant the bastion unnecessary compute/registry/VPC admin roles.

---

## Decision 3: Migration tool — custom Go binary vs. standalone goose CLI

**Decision**: Create `cmd/migrate/main.go` — a small Go CLI that opens a YDB connection using `yc.WithInstanceServiceAccount()` + `yc.WithInternalCA()` and runs goose migrations programmatically (same pattern as `testhelper/ydb.go`). Update `Dockerfile.migrations` to build and ship this binary instead of the standalone `goose` CLI.
**Rationale**:
- The standalone `goose` CLI has no built-in knowledge of Yandex Cloud metadata-based IAM. Auth would require manually fetching a token from the metadata endpoint and injecting it as an env var — fragile, requires shell, and doesn't handle token expiry.
- `yc.WithInstanceServiceAccount()` from `ydb-go-yc` automatically refreshes tokens from the VM metadata service — the same mechanism already used by worker/producer containers.
- `cmd/migrate/main.go` is a natural addition to the repo (`cmd/` pattern established by `04_coordinated_table/cmd`).
**Alternatives considered**:
- Standalone `goose` + `YDB_ACCESS_TOKEN_CREDENTIALS` env var injection via SSH — rejected: requires shell in final image, manual token fetch, no refresh.
- `goose` CLI with SA key file — rejected: requires distributing a key file to the bastion, violates intent of SA attachment.

---

## Decision 4: SSH provisioner connectivity

**Decision**: Use `provisioner "remote-exec"` with a `connection` block targeting the bastion's static NAT IP (`111.88.240.80`) using `type = "ssh"`, `user = "yc-user"` (the default COI user), and `private_key = file(var.ssh_private_key_path)`.
**Rationale**: The bastion already has a static external IP configured. COI VMs use `yc-user` as the default SSH user (consistent with `cloud-init.yaml` which creates `yc-user`). A new `ssh_private_key_path` variable is added to the root module to supply the private key path without hardcoding.
**Alternatives considered**: Dynamic IP lookup — rejected because the bastion already has a static IP reserved.

---

## Decision 5: Idempotency of SSH provisioner

**Decision**: Terraform's `provisioner "remote-exec"` only fires on resource creation (first `terraform apply` that creates the VM). On subsequent applies with no VM changes, the provisioner does not re-run. Goose's `Up` command is also inherently idempotent (skips already-applied migrations). No extra guard needed.
**Rationale**: Terraform provisioner lifecycle guarantees this by default. If the VM is force-replaced (e.g., `terraform taint`), migrations re-run — which is safe because goose `Up` is idempotent.
**Alternatives considered**: `null_resource` with `triggers` — rejected as unnecessary complexity when the provisioner on the compute resource suffices.

---

## Decision 6: Migration image build and push

**Decision**: Add a `null_resource` with a `local-exec` provisioner to build and push `Dockerfile.migrations` to the container registry before the bastion VM is created. The bastion VM resource gets a `depends_on` on this null_resource to ensure the image is present before the SSH provisioner pulls it.
**Rationale**: Other modules (workers, producer) already follow this pattern via the `dirhash` trigger and `null_resource` pattern visible in `modules/producer/compute.tf` and `modules/workers/compute.tf`. Consistent approach.
**Alternatives considered**: Pre-built image in CI — possible but out of scope for this feature; the Terraform-driven build keeps things self-contained.

---

## Decision 7: Migration container auth to registry

**Decision**: The bastion SA needs `container-registry.images.puller` role in addition to `ydb.editor` so it can pull the migration image from the private registry.
**Rationale**: Without this role, `docker pull` on the bastion will fail with a 403. The bastion SA authenticates to the registry using its IAM token (same metadata service).
**Alternatives considered**: Make the registry public — rejected; the existing registry is private and the pattern is to use SA tokens.
