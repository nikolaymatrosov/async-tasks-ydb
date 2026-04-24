# Tasks: Bastion VM Migrations via COI + SSH Provisioner

**Input**: Design documents from `/specs/012-bastion-vm-migrations/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup

**Purpose**: Create the Go migration binary and update the migration container image

- [X] T001 Create `cmd/migrate/main.go` with: YDB connection via `ydb.Open`, metadata-service credentials via `yc.WithMetadataCredentials()` (fallback when `YDB_SA_KEY_FILE` absent), `yc.WithInternalCA()`, goose provider with `goose.DialectYdB` + `ScriptingQueryMode` + `FakeTx` + `AutoDeclare` + `NumericArgs`, `slog.NewJSONHandler` structured logging to stderr, and `provider.Up(ctx)` call — per plan.md implementation notes
- [X] T002 Rewrite `Dockerfile.migrations` as multi-stage build: builder stage copies `go.mod`/`go.sum`, runs `go build -ldflags="-w -s" -o /app/migrate ./cmd/migrate/`, final stage uses `gcr.io/distroless/static-debian12:nonroot` with `/app/migrate` binary and `migrations/` dir — removing the standalone goose install and `root.crt` copy per plan.md Dockerfile.migrations section

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: IAM module additions and root variable that `bastion.tf` references — must be in place before user story HCL changes compile

**⚠️ CRITICAL**: All bastion.tf user story tasks depend on this phase

- [X] T003 Append `yandex_iam_service_account.bastion` (name = "async-tasks-bastion-sa"), `yandex_resourcemanager_folder_iam_member.bastion_ydb_editor` (role = "ydb.editor"), and `yandex_resourcemanager_folder_iam_member.bastion_registry_puller` (role = "container-registry.images.puller") to `terraform/modules/db/iam.tf`
- [X] T004 [P] Append `bastion_service_account_id` output block (value = `yandex_iam_service_account.bastion.id`, description = "ID of the bastion VM service account") to `terraform/modules/db/outputs.tf`
- [X] T005 [P] Append `ssh_private_key_path` variable block (type = string, default = "~/.ssh/id_rsa", description = "Local path to the SSH private key used by the Terraform SSH provisioner on the bastion") to `terraform/variables.tf`

**Checkpoint**: Foundation ready — user story implementation can now begin

---

## Phase 3: User Story 1 — Run migrations on infrastructure apply (Priority: P1) 🎯 MVP

**Goal**: `terraform apply` automatically executes the YDB migration container via SSH on first VM creation; idempotent on re-apply when the VM exists unchanged

**Independent Test**: After `terraform apply` completes, query YDB to confirm all migration-created tables (`tasks`, `stats`, `processed`, `coordinated_tasks`) exist without any manual operator steps

**⚠️ Note**: Implement US2 (Phase 4) and US3 (Phase 5) before this phase — T006 references attributes introduced by those phases

### Implementation for User Story 1

- [X] T006 [US1] Add `connection` block (`type = "ssh"`, `user = "yc-user"`, `host = self.network_interface[0].nat_ip_address`, `private_key = file(var.ssh_private_key_path)`) and `provisioner "remote-exec"` block (inline: `docker run --rm -e YDB_ENDPOINT='${module.db.ydb_endpoint}' ${module.workers.migrations_image}`) and `depends_on = [module.workers]` to `yandex_compute_instance.bastion` in `terraform/bastion.tf`

**Checkpoint**: US1 ready — run `terraform taint yandex_compute_instance.bastion && terraform apply` and verify migration tables exist in YDB

---

## Phase 4: User Story 2 — Bastion uses COI image (Priority: P2)

**Goal**: The bastion VM boots from Container-Optimized Image with Docker pre-installed, eliminating manual tooling setup and removing all Ubuntu image references

**Independent Test**: SSH into the bastion and confirm `docker version` succeeds; `terraform plan` output contains no reference to `ubuntu-2404-lts`

### Implementation for User Story 2

- [X] T007 [US2] Replace the `data "yandex_compute_image" "ubuntu"` block (family = "ubuntu-2404-lts") with `data "yandex_compute_image" "coi_bastion"` (family = "container-optimized-image") in `terraform/bastion.tf`
- [X] T008 [US2] Update `yandex_compute_instance.bastion` in `terraform/bastion.tf`: change `boot_disk.initialize_params.image_id` from `data.yandex_compute_image.ubuntu.id` to `data.yandex_compute_image.coi_bastion.id`, change `boot_disk.initialize_params.size` from 20 to 10, and change `metadata.ssh-keys` user prefix from `ubuntu` to `yc-user`

**Checkpoint**: US2 ready — run `terraform validate` and confirm no references to the ubuntu image remain in the plan

---

## Phase 5: User Story 3 — Service account attached to bastion (Priority: P3)

**Goal**: The bastion VM has a dedicated service account with `ydb.editor` + `container-registry.images.puller` roles; the migration tool retrieves an IAM token from the metadata service without static credentials

**Independent Test**: SSH into the bastion and run `curl -H Metadata-Flavor:Google http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token` — receive a valid IAM token JSON

### Implementation for User Story 3

- [X] T009 [US3] Add `service_account_id = module.db.bastion_service_account_id` to `yandex_compute_instance.bastion` in `terraform/bastion.tf`

**Checkpoint**: US3 ready — after apply, verify the metadata token endpoint returns a valid IAM token on the bastion VM

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Build gate and final Terraform validation

- [X] T010 [P] Run `go vet ./cmd/migrate/` to confirm the migration binary compiles without errors
- [X] T011 [P] Run `terraform validate` in `terraform/` to confirm HCL syntax and all module cross-references resolve correctly
- [ ] T012 Run `terraform plan` against the live folder and review that the only planned changes are: bastion VM recreation, new `bastion` SA, two IAM bindings, and one new module output

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: No dependencies — can run in parallel with Phase 1
- **User Stories (Phases 3–5)**: All depend on Foundational phase completion
  - **US3 (Phase 5)**: Depends only on Foundation (T003–T005)
  - **US2 (Phase 4)**: Depends only on Foundation (T003–T005)
  - **US1 (Phase 3)**: Depends on Foundation + US2 + US3 (all `bastion.tf` changes must be present)
- **Polish (Phase 6)**: Depends on all implementation phases complete

### Recommended Implementation Order

```text
Phase 1 (Setup) ────────┐
                         ├──→ Phase 5 (US3) ──┐
Phase 2 (Foundation) ───┤                     ├──→ Phase 3 (US1) ──→ Phase 6 (Polish)
                         └──→ Phase 4 (US2) ──┘
```

US2 and US3 can be implemented sequentially in either order (both edit `bastion.tf` — avoid concurrent edits to the same file).

### Within Each Story

- T004 and T005 can run in parallel (different files)
- T007 before T008 within US2 (T008 updates the reference introduced by T007)
- T009 (US3) depends on T003/T004 being written (module output must exist)
- T006 (US1) depends on T007/T008/T009 all being complete (all referenced attributes must be defined)

---

## Parallel Example: Foundational Phase

```bash
# These can run in parallel (different files):
Task T004: "Append bastion_service_account_id output to terraform/modules/db/outputs.tf"
Task T005: "Append ssh_private_key_path variable to terraform/variables.tf"
```

---

## Implementation Strategy

### MVP First

1. Complete Phase 1: Setup (T001, T002)
2. Complete Phase 2: Foundational (T003, T004, T005)
3. Complete Phase 5 (US3): SA resources in iam.tf + SA attachment
4. Complete Phase 4 (US2): COI image data source + bastion.tf image update
5. Complete Phase 3 (US1): SSH provisioner block
6. **STOP and VALIDATE**: `terraform taint yandex_compute_instance.bastion && terraform apply` — confirm YDB tables exist post-apply
7. Complete Phase 6: Polish (build gate + validate + plan review)

### Incremental Delivery

1. Setup + Foundation → binary builds, IAM resources defined
2. US3 → service account provisioned, metadata token accessible from bastion
3. US2 → bastion boots from COI, Docker available without manual install
4. US1 → end-to-end migration flow: `terraform apply` runs migrations automatically
5. Polish → validation gate passes

---

## Notes

- [P] tasks = different files, no shared state — safe to implement in parallel
- [Story] label maps each task to its user story for traceability
- US1 (P1) is the primary business goal; US2 and US3 are enabling prerequisites
- `terraform/bastion.tf` is touched by all three user stories — implement sequentially to avoid conflicts
- No new Go dependencies or Terraform providers needed — all already in `go.mod` / `.terraform.lock.hcl`
- The bastion SA (`async-tasks-bastion-sa`) is intentionally separate from `coi-vm-sa` to limit blast radius per research.md Decision 2
