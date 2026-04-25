# Tasks: Per-IG Service Account Isolation with Safe Dependency Ordering

**Input**: Design documents from `specs/015-tf-ig-sa-refactor/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅

**Organization**: Tasks are grouped by user story to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)

---

## Phase 1: Setup (Baseline Verification)

**Purpose**: Confirm the current Terraform configuration is valid before making any changes.

- [ ] T001 Run `terraform validate` in `terraform/` and confirm zero errors (baseline checkpoint)
- [ ] T002 Run `terraform plan` and note current resource count for comparison after refactor

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Remove the shared `coi_vm` SA and its IAM bindings from the `db` module, and remove the shared `service_account_id` wire in `main.tf`. Both US1 and US2 depend on these being gone before they can be validated end-to-end.

**⚠️ CRITICAL**: US1 and US2 implementation can proceed in parallel with this phase (editing different files), but `terraform validate` of the full configuration cannot pass until this phase is complete.

- [ ] T003 In `terraform/modules/db/iam.tf`: remove `yandex_iam_service_account.coi_vm` resource and its 7 `yandex_resourcemanager_folder_iam_member` bindings (`registry_puller`, `ydb_editor`, `monitoring_editor`, `vpc_user`, `vpc_public_admin`, `compute_editor`, `iam_sa_user`); retain `yandex_iam_service_account.bastion` and its 2 bindings unchanged
- [ ] T004 In `terraform/modules/db/outputs.tf`: remove the `service_account_id` output block entirely
- [ ] T005 In `terraform/main.tf`: remove `service_account_id = module.db.service_account_id` from the `module "workers"` block and from the `module "producer"` block

**Checkpoint**: Foundation ready — `db` module no longer owns the shared SA; US1 and US2 can now complete end-to-end

---

## Phase 3: User Story 1 — Producer IG Service Account Isolation (Priority: P1) 🎯 MVP

**Goal**: Producer IG uses a dedicated manager SA and a dedicated VM SA, both defined within the producer module, with correct `depends_on` ordering.

**Independent Test**: After applying changes to the producer module only (`terraform apply -target module.producer`), verify two SAs named `async-tasks-producer-ig-sa` and `async-tasks-producer-vm-sa` exist in the folder, the producer IG references them correctly, and producer VMs pull images and connect to YDB without IAM errors.

### Implementation for User Story 1

- [ ] T006 [P] [US1] Create `terraform/modules/producer/iam.tf` with `yandex_iam_service_account.producer_ig` (name: `async-tasks-producer-ig-sa`) and `yandex_iam_service_account.producer_vm` (name: `async-tasks-producer-vm-sa`), plus 4 IAM bindings for `producer_ig` (`compute.editor`, `iam.serviceAccounts.user`, `vpc.user`, `vpc.publicAdmin`) and 3 IAM bindings for `producer_vm` (`container-registry.images.puller`, `ydb.editor`, `monitoring.editor`); all bindings use `var.folder_id` and `yandex_resourcemanager_folder_iam_member` resource type
- [ ] T007 [US1] In `terraform/modules/producer/compute.tf`: replace both `service_account_id = var.service_account_id` references — IG-level with `yandex_iam_service_account.producer_ig.id` and instance-template-level with `yandex_iam_service_account.producer_vm.id`; add `depends_on` to `yandex_compute_instance_group.producer` listing all 7 IAM binding resources from T006 alongside the existing `null_resource.coordinator_image` entry (depends on T006)
- [ ] T008 [P] [US1] In `terraform/modules/producer/variables.tf`: remove the `service_account_id` variable block entirely

**Checkpoint**: Producer IG fully isolated — two dedicated SAs, correct SA references in IG resource, `depends_on` wiring in place

---

## Phase 4: User Story 2 — Workers IG Service Account Isolation (Priority: P1)

**Goal**: Workers IG uses a dedicated manager SA and a dedicated VM SA, both defined within the workers module, with correct `depends_on` ordering. Autoscale operations use the manager SA.

**Independent Test**: After applying changes to the workers module only (`terraform apply -target module.workers`), verify two SAs named `async-tasks-workers-ig-sa` and `async-tasks-workers-vm-sa` exist in the folder, the workers IG references them correctly, autoscale events complete without IAM errors, and worker VMs connect to YDB without IAM errors.

### Implementation for User Story 2

- [ ] T009 [P] [US2] Create `terraform/modules/workers/iam.tf` with `yandex_iam_service_account.workers_ig` (name: `async-tasks-workers-ig-sa`) and `yandex_iam_service_account.workers_vm` (name: `async-tasks-workers-vm-sa`), plus 4 IAM bindings for `workers_ig` (`compute.editor`, `iam.serviceAccounts.user`, `vpc.user`, `vpc.publicAdmin`) and 3 IAM bindings for `workers_vm` (`container-registry.images.puller`, `ydb.editor`, `monitoring.editor`); all bindings use `var.folder_id` and `yandex_resourcemanager_folder_iam_member` resource type
- [ ] T010 [US2] In `terraform/modules/workers/compute.tf`: replace both `service_account_id = var.service_account_id` references — IG-level with `yandex_iam_service_account.workers_ig.id` and instance-template-level with `yandex_iam_service_account.workers_vm.id`; add `depends_on` to `yandex_compute_instance_group.workers` listing all 7 IAM binding resources from T009 (depends on T009)
- [ ] T011 [P] [US2] In `terraform/modules/workers/variables.tf`: remove the `service_account_id` variable block entirely

**Checkpoint**: Workers IG fully isolated — two dedicated SAs, correct SA references in IG resource, `depends_on` wiring in place

---

## Phase 5: User Story 3 — Safe IAM Lifecycle Validation (Priority: P2)

**Goal**: Confirm that `terraform plan` and `terraform apply` produce the correct resource creation/destroy ordering so no IG is ever left without its required IAM permissions.

**Independent Test**: Inspect `terraform plan -destroy` output and confirm both `yandex_compute_instance_group` resources appear as destroyed before any `yandex_resourcemanager_folder_iam_member` resources for their respective SAs.

**⚠️ CRITICAL**: Depends on T003–T011 (Phases 2–4) being complete.

### Implementation for User Story 3

- [ ] T012 [US3] Run `terraform validate` in `terraform/`; must exit 0 with no errors (depends on T003–T011 all complete)
- [ ] T013 [US3] Run `terraform plan` and verify the plan shows: CREATE 4 new SAs, CREATE 14 new IAM bindings, DESTROY 1 old SA (`coi_vm`), DESTROY 7 old IAM bindings from db module, UPDATE 2 `yandex_compute_instance_group` resources; note any unexpected diffs
- [ ] T014 [US3] Run `terraform plan -destroy 2>&1 | grep -E "(yandex_compute_instance_group|yandex_resourcemanager_folder_iam_member|yandex_iam_service_account)"` and confirm both IGs appear before IAM binding removals in the destroy sequence
- [ ] T015 [US3] Run `terraform apply` against the live Yandex Cloud environment; confirm apply completes without errors
- [ ] T016 [US3] Verify post-apply: list folder SAs and confirm exactly 4 new SAs exist (`async-tasks-producer-ig-sa`, `async-tasks-producer-vm-sa`, `async-tasks-workers-ig-sa`, `async-tasks-workers-vm-sa`) and `coi-vm-sa` is absent
- [ ] T017 [US3] Verify VMs in both IGs are healthy: check that producer and worker VMs pull images, connect to YDB, and emit metrics without IAM-related errors in instance logs

**Checkpoint**: All three user stories validated — safe destroy ordering confirmed, VMs operational with isolated SAs

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Code hygiene and final verification.

- [ ] T018 [P] Run `terraform fmt -recursive terraform/` and commit any formatting fixes
- [ ] T019 Run `terraform validate` one final time to confirm idempotent clean state
- [ ] T020 Run `terraform plan` against live environment and confirm zero diffs (no unexpected drift)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — run immediately
- **Foundational (Phase 2)**: Depends on Phase 1; BLOCKS full end-to-end validation of US1 and US2 but does NOT block writing HCL for US1/US2 (different files)
- **US1 (Phase 3)**: T006 and T008 can start immediately (new file, different variable file); T007 depends on T006
- **US2 (Phase 4)**: T009 and T011 can start immediately (new file, different variable file); T010 depends on T009
- **US3 (Phase 5)**: Depends on T003–T011 all complete
- **Polish (Phase 6)**: Depends on Phase 5 complete

### User Story Dependencies

- **US1 (P1)**: Independent — producer module files only
- **US2 (P1)**: Independent — workers module files only; can be worked in parallel with US1
- **US3 (P2)**: Depends on US1 + US2 + Foundational phase complete; validates the combined result

### Parallel Opportunities

- T003, T006, T009 can all start simultaneously (different files: db/iam.tf, producer/iam.tf, workers/iam.tf)
- T004, T008, T011 can run in parallel (different files: db/outputs.tf, producer/variables.tf, workers/variables.tf)
- T005 can run in parallel with T006–T011 (main.tf only)
- T018, T019 can run in parallel in the Polish phase

---

## Parallel Example: US1 + US2 Together

```bash
# These can all start simultaneously (no shared file conflicts):
Task T003: Remove coi_vm SA from terraform/modules/db/iam.tf
Task T006: Create terraform/modules/producer/iam.tf
Task T009: Create terraform/modules/workers/iam.tf

# Then in parallel:
Task T004: Remove service_account_id output from terraform/modules/db/outputs.tf
Task T007: Update terraform/modules/producer/compute.tf (depends on T006)
Task T008: Remove variable from terraform/modules/producer/variables.tf
Task T010: Update terraform/modules/workers/compute.tf (depends on T009)
Task T011: Remove variable from terraform/modules/workers/variables.tf

# Then:
Task T005: Remove service_account_id from terraform/main.tf
```

---

## Implementation Strategy

### MVP First (US1 Only)

1. Complete Phase 1: Baseline validate
2. Complete T003, T004, T005 (Foundational — db module cleanup)
3. Complete T006, T007, T008 (US1 — producer isolation)
4. **STOP and VALIDATE**: `terraform validate`, plan review, targeted apply for producer module
5. Proceed to US2

### Incremental Delivery

1. Phase 1 + Phase 2 → db module clean
2. Phase 3 (US1) → Producer isolated → Validate producer independently
3. Phase 4 (US2) → Workers isolated → Validate workers independently
4. Phase 5 (US3) → Full apply + destroy ordering confirmed
5. Phase 6 → Polish

---

## Notes

- `depends_on` belongs on the `yandex_compute_instance_group` resource (not on IAM bindings) — this ensures IG is destroyed before bindings are removed
- SA names must be ≤ 63 chars, lowercase alphanumeric + hyphens — all proposed names comply
- `yandex_resourcemanager_folder_iam_member` is the correct resource type for folder-level role bindings in the Yandex provider
- No new Terraform providers introduced
- The `bastion` SA in `db/iam.tf` is out of scope — do not modify it
