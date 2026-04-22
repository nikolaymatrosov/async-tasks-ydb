# Tasks: Terraform Modular Deployment

**Input**: Design documents from `/specs/007-tf-modular-deploy/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/module-interfaces.md ✅, quickstart.md ✅

**Organization**: Tasks grouped by user story for independent implementation and testing of each story.
**Tests**: Not applicable — testing is manual `terraform plan -target=module.<name>` against a live cloud folder per plan.md.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1=db, US2=workers, US3=producer, US4=full-stack)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the module directory scaffold so child modules can be written independently.

- [X] T001 Create module directory scaffold: `terraform/modules/db/`, `terraform/modules/workers/`, `terraform/modules/producer/`

---

## Phase 2: User Story 1 — Deploy Database Independently (Priority: P1) 🎯 MVP

**Goal**: Extract all shared foundational resources (network, YDB, registry, IAM) into a standalone child module that can be applied with `terraform apply -target=module.db`.

**Independent Test**: Run `terraform apply -target=module.db` and verify YDB endpoint is reachable, container registry exists, and no compute instance groups were created.

### Implementation for User Story 1

- [X] T002 [US1] Create `terraform/modules/db/variables.tf` — declare all inputs per contract: `cloud_id`, `folder_id`, `zone`, `subnet_cidrs`, `ydb_name`, `ydb_resource_preset`, `ydb_fixed_size`, `ydb_storage_type`, `ydb_storage_groups`, `registry_name` (all with defaults matching existing `terraform/variables.tf`)
- [X] T003 [P] [US1] Create `terraform/modules/db/network.tf` — migrate `yandex_vpc_network.main` and `yandex_vpc_subnet.main` (for_each over `var.subnet_cidrs`) from `terraform/network.tf`; update all references from bare vars to `var.*`
- [X] T004 [P] [US1] Create `terraform/modules/db/ydb.tf` — migrate `yandex_ydb_database_dedicated.main` and `data "dirhash_sha256" "migrations"` from `terraform/ydb.tf`; subnet reference becomes `[for s in yandex_vpc_subnet.main : s.id]`
- [X] T005 [P] [US1] Create `terraform/modules/db/registry.tf` — migrate `yandex_container_registry.main` from `terraform/registry.tf`; use `var.registry_name`
- [X] T006 [P] [US1] Create `terraform/modules/db/iam.tf` — migrate `yandex_iam_service_account.coi_vm` and all 7 `yandex_resourcemanager_folder_iam_member.*` bindings from `terraform/iam.tf`; use `var.folder_id`
- [X] T007 [US1] Create `terraform/modules/db/outputs.tf` — declare all 7 outputs per contract: `ydb_endpoint` (ydb_full_endpoint), `ydb_database_path`, `registry_id`, `registry_url` (`"cr.yandex/${yandex_container_registry.main.id}"`), `service_account_id`, `subnet_ids` (list comprehension), `network_id`

**Checkpoint**: `terraform/modules/db/` is a valid, complete Terraform module targeting db resources only.

---

## Phase 3: User Story 2 — Deploy Workers Independently (Priority: P2)

**Goal**: Extract worker instance group and container image builds into a child module consuming `module.db` outputs, deployable with `terraform apply -target=module.workers`.

**Independent Test**: With db module already deployed, run `terraform apply -target=module.workers`; verify `terraform output instance_group_id` is non-empty and workers connect to YDB endpoint.

### Implementation for User Story 2

- [X] T008 [US2] Create `terraform/modules/workers/variables.tf` — declare all inputs per contract: required (`folder_id`, `registry_url`, `service_account_id`, `ydb_endpoint`, `ydb_database`, `subnet_ids`) and optional (`zone`, `platform_id`, `vm_cores`, `vm_memory`, `ssh_public_key`, `ig_max_size`, `ig_min_zone_size`, `ig_cpu_target`, `ig_stabilization_duration`, `ig_warmup_duration`, `ig_measurement_duration`, `worker_rate`)
- [X] T009 [P] [US2] Create `terraform/modules/workers/containers.tf` — include `data "external" "git_hash"`, locals for `coordinator_image`, `cdc_worker_image`, `topic_bench_image`, `migrations_image` (using `var.registry_url` not a local registry_url); migrate `null_resource` image builds for `coordinator_image`, `cdc_worker_image`, `topic_bench_image`, `migrations_image` from `terraform/containers.tf` (excludes `db_producer_image`); update `path.module` references to `${path.module}/..` (root of repo)
- [X] T010 [US2] Create `terraform/modules/workers/compute.tf` — migrate `data "yandex_compute_image" "coi"` and `yandex_compute_instance_group.workers` from `terraform/compute.tf`; replace all bare resource/var references with module variables (`var.subnet_ids`, `var.service_account_id`, `var.ydb_endpoint`, `var.ydb_database`, etc.); `docker-compose` templatefile uses `${path.module}/docker-compose.yml.tpl`
- [X] T011 [P] [US2] Create `terraform/modules/workers/docker-compose.yml.tpl` — copy from `terraform/docker-compose.yml.tpl`; template variables (`coordinator_image`, `ydb_endpoint`, `ydb_database`, `worker_rate`) are identical; no changes needed
- [X] T012 [P] [US2] Create `terraform/modules/workers/ua-config.yml.tpl` — copy from `terraform/ua-config.yml.tpl`; no changes needed
- [X] T013 [US2] Create `terraform/modules/workers/outputs.tf` — declare all 6 outputs per contract: `instance_group_id`, `coordinator_image`, `cdc_worker_image`, `topic_bench_image`, `migrations_image`, `migrations_run_cmd` (docker run command referencing `local.migrations_image` and `var.ydb_endpoint`)

**Checkpoint**: `terraform/modules/workers/` is a valid, complete Terraform module; a worker-only apply produces a compute instance group and no other resources.

---

## Phase 4: User Story 3 — Deploy Producer Independently (Priority: P2)

**Goal**: Create a new `producer` child module that builds and deploys the `01_db_producer` container as a fixed-scale instance group, deployable with `terraform apply -target=module.producer`.

**Independent Test**: With db module already deployed, run `terraform apply -target=module.producer`; verify `terraform output producer_instance_group_id` is non-empty and producer VMs are writing tasks to YDB.

### Implementation for User Story 3

- [X] T014 [US3] Create `terraform/modules/producer/variables.tf` — declare all inputs per contract: required (`folder_id`, `registry_url`, `service_account_id`, `ydb_endpoint`, `ydb_database`, `subnet_ids`) and optional (`zone`, `platform_id`, `vm_cores`, `vm_memory`, `ssh_public_key`, `producer_size` default 1, `producer_parallelism` default 10)
- [X] T015 [P] [US3] Create `terraform/modules/producer/containers.tf` — include `data "external" "git_hash"` (same program as workers), local `db_producer_image = "${var.registry_url}/db-producer:${data.external.git_hash.result.sha}"`, and `null_resource "db_producer_image"` triggered on git_sha; build command: `cd ${path.module}/.. && docker build --platform linux/amd64 --build-arg EXAMPLE=01_db_producer -t ${local.db_producer_image} . && docker push ${local.db_producer_image}`
- [X] T016 [US3] Create `terraform/modules/producer/compute.tf` — include `data "yandex_compute_image" "coi"` and `yandex_compute_instance_group.producer` with `fixed_scale { size = var.producer_size }`; metadata uses `docker-compose` templatefile (`${path.module}/docker-compose.yml.tpl`) with `db_producer_image`, `ydb_endpoint`, `ydb_database`, `producer_parallelism`; network_interface uses `var.subnet_ids`, service_account uses `var.service_account_id`; depends_on `null_resource.db_producer_image`
- [X] T017 [P] [US3] Create `terraform/modules/producer/docker-compose.yml.tpl` — docker-compose v3 service `db-producer` running `var.db_producer_image` with env vars `YDB_ENDPOINT` and `YDB_DATABASE`, command `["--parallelism", "${producer_parallelism}"]`, `restart: unless-stopped`
- [X] T018 [US3] Create `terraform/modules/producer/outputs.tf` — declare `producer_instance_group_id` (yandex_compute_instance_group.producer.id) and `db_producer_image` (local.db_producer_image)

**Checkpoint**: `terraform/modules/producer/` is a valid, complete Terraform module; a producer-only apply deploys a fixed-scale instance group running db-producer.

---

## Phase 5: User Story 4 — Deploy Full Stack at Once (Priority: P3)

**Goal**: Rewrite the root module to compose all three child modules, preserve backward-compatible outputs, and add new producer variables — so `terraform apply` with no target deploys everything in dependency order.

**Independent Test**: Run `terraform apply` (no target) on a fresh folder; verify all outputs are present including the new `producer_instance_group_id`.

### Implementation for User Story 4

- [X] T019 [US4] Rewrite `terraform/main.tf` — keep existing `terraform {}` block and `provider "yandex"` unchanged; add `module "db"` block passing all db vars, `module "workers"` block passing worker vars + db outputs (`registry_url = module.db.registry_url`, `service_account_id = module.db.service_account_id`, `ydb_endpoint = module.db.ydb_endpoint`, `ydb_database = module.db.ydb_database_path`, `subnet_ids = module.db.subnet_ids`), and `module "producer"` block with same db output wiring plus `producer_size` and `producer_parallelism`
- [X] T020 [P] [US4] Update `terraform/variables.tf` — append two new variables at end: `producer_size` (number, default 1, description "Fixed number of producer VMs") and `producer_parallelism` (number, default 10, description "Maps to --parallelism flag in db-producer"); existing variables unchanged
- [X] T021 [US4] Rewrite `terraform/outputs.tf` — map every existing output to its child module source per the backward-compatibility table in contracts/module-interfaces.md; add new `producer_instance_group_id` output from `module.producer.producer_instance_group_id`; remove all direct resource references
- [X] T022 [P] [US4] Update `terraform/terraform.tfvars.example` — append a `# Producer instance group` section documenting `producer_size = 1` and `producer_parallelism = 10` with descriptions; existing entries unchanged

**Checkpoint**: Root module composes all three child modules; `terraform plan` (no target) shows resources across all three modules; existing `terraform.tfvars` works without modification.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Remove now-redundant flat resource files from `terraform/` root, format all HCL, and validate the complete module tree.

- [X] T023 [P] Delete flat resource files now owned by child modules: `terraform/network.tf`, `terraform/ydb.tf`, `terraform/registry.tf`, `terraform/iam.tf`, `terraform/compute.tf`, `terraform/containers.tf`, `terraform/docker-compose.yml.tpl`, `terraform/ua-config.yml.tpl`
- [X] T024 [P] Run `terraform fmt -recursive terraform/` to normalize HCL formatting across all files
- [X] T025 Run `terraform validate` in `terraform/` to confirm no syntax errors (requires `terraform init` first if providers not cached)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **User Story 1 / db module (Phase 2)**: Depends on Phase 1; BLOCKS all user story phases since workers and producer receive db outputs
- **User Story 2 / workers module (Phase 3)**: Can start after Phase 2 (db variables.tf + outputs.tf define the interface)
- **User Story 3 / producer module (Phase 4)**: Can start after Phase 2 (same db interface); independent of Phase 3
- **User Story 4 / root rewire (Phase 5)**: Depends on Phases 2, 3, and 4 (all child modules must exist before root wires them)
- **Polish (Phase 6)**: Depends on Phase 5 completion

### User Story Dependencies

- **US1 (db)**: No dependencies — start after Setup
- **US2 (workers)**: Depends on US1 db interface definition (T002 + T007) — no recompile needed, HCL is checked at plan time
- **US3 (producer)**: Depends on US1 db interface definition (T002 + T007) — independent of US2
- **US4 (root)**: Depends on US1 + US2 + US3 all complete

### Within Each User Story

- `variables.tf` before `compute.tf` (compute references variables)
- `containers.tf` before `compute.tf` (compute depends_on image builds)
- Resource files before `outputs.tf` (outputs reference resources)
- Templates [P] are independent of all other files in their module

### Parallel Opportunities

- T003–T006 (db resource files) can all run in parallel after T002
- T009, T011, T012 (workers templates/containers) can run in parallel after T008
- T014–T015, T017 (producer containers/templates) can run in parallel
- T020 and T022 (root variables.tf + tfvars.example) can run in parallel with T019 and T021
- T023 and T024 (delete + format) can run in parallel

---

## Parallel Example: User Story 1 (db module)

```bash
# After T002 (variables.tf), launch all resource files together:
Task: T003 — network.tf
Task: T004 — ydb.tf
Task: T005 — registry.tf
Task: T006 — iam.tf
# Then T007 (outputs.tf) after all resource files complete
```

## Parallel Example: User Stories 2 and 3

```bash
# Once T007 (db outputs.tf) is done, run both in parallel:
Task: T008–T013 (workers module, Phase 3)
Task: T014–T018 (producer module, Phase 4)
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001)
2. Complete Phase 2: db module (T002–T007)
3. **STOP and VALIDATE**: `terraform apply -target=module.db` succeeds
4. Continue to workers and producer

### Incremental Delivery

1. Setup → db module → `terraform apply -target=module.db` ✅
2. Add workers module → `terraform apply -target=module.workers` ✅
3. Add producer module → `terraform apply -target=module.producer` ✅
4. Root rewire → `terraform apply` (no target) ✅
5. Cleanup flat files + validate ✅

### Key Invariants to Preserve

- Root `terraform/variables.tf` and `terraform.tfvars` must stay backward-compatible (SC-006)
- All existing outputs in `terraform/outputs.tf` must survive (SC-005)
- No compute resources in the db module (SC-001 invariant)
- IAM resources owned exclusively by db module — not duplicated in workers/producer (FR-008)
- `path.module` in container build commands must resolve to `terraform/modules/<name>/`, so `${path.module}/..` reaches the repo root where `Dockerfile` lives

---

## Notes

- [P] tasks = different files, no file-level conflicts
- This is a pure HCL reorganisation — no Go code changes
- The `.terraform/` providers directory and state remain at `terraform/` root; `terraform init` is required after restructuring
- Existing `terraform.tfvars` requires no changes; all new variables have defaults
- `migrations_run_cmd` in workers outputs references `var.ydb_endpoint` (not `yandex_ydb_database_dedicated.main.ydb_full_endpoint`) since the workers module no longer owns the YDB resource
