# Tasks: Terraform Infrastructure for YDB Cluster and Container-Optimized VMs

**Input**: Design documents from `specs/003-terraform-ydb-infra/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Not requested in feature specification. Manual validation per constitution (Development Workflow).

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the Terraform project structure and shared configuration files

- [x] T001 Create `terraform/` directory and initialize Terraform project with `yandex-cloud/yandex` provider in `terraform/main.tf`
- [x] T002 Define all input variables per contracts/terraform-variables.md in `terraform/variables.tf` (cloud_id, folder_id, sa_key_file, zone, vm_cores, vm_memory, ssh_public_key, ydb_name, registry_name)
- [x] T003 [P] Create `terraform/terraform.tfvars.example` with placeholder values and comments for required variables
- [x] T004 [P] Define all output values per contracts/terraform-outputs.md in `terraform/outputs.tf` (ydb_endpoint, ydb_database_path, registry_id, vm_external_ip, vm_internal_ip, service_account_id)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core Terraform resources that MUST be complete before any user story can be implemented. These resources are referenced by resources in later phases.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T005 Create VPC network resource (`yandex_vpc_network`) in `terraform/network.tf`
- [x] T006 Create VPC subnet resource (`yandex_vpc_subnet`) in the same `terraform/network.tf` — zone from variable, CIDR `10.128.0.0/24`, references network from T005
- [x] T007 [P] Create service account resource (`yandex_iam_service_account`) in `terraform/iam.tf`
- [x] T008 Add IAM role binding `container-registry.images.puller` for service account in `terraform/iam.tf` (depends on T007)
- [x] T009 Add IAM role binding `ydb.editor` for service account in `terraform/iam.tf` (depends on T007)

**Checkpoint**: Network, subnet, and service account with correct IAM roles are ready. Run `terraform plan` to verify no errors.

---

## Phase 3: User Story 1 — Provision Full Infrastructure (Priority: P1) 🎯 MVP

**Goal**: A developer can provision the complete infrastructure (YDB, container registry, VM) with a single `terraform apply` and tear it down with `terraform destroy`.

**Independent Test**: Run `terraform apply` in a clean Yandex Cloud folder, then verify all resources appear in the cloud console. Run `terraform destroy` and verify no orphaned resources remain.

### Implementation for User Story 1

- [x] T010 [P] [US1] Create YDB Serverless database resource (`yandex_ydb_database_serverless`) in `terraform/ydb.tf` — name and folder_id from variables, location_id = `ru-central1`
- [x] T011 [P] [US1] Create container registry resource (`yandex_container_registry`) in `terraform/registry.tf` — name from variable, folder_id from variable
- [x] T012 [US1] Create COI VM data source (`data.yandex_compute_image` with `family = "container-optimized-image"`) in `terraform/compute.tf`
- [x] T013 [US1] Create compute instance resource (`yandex_compute_instance`) in `terraform/compute.tf` — boot disk from COI image (T012), network interface with subnet (T006) and NAT enabled, service account (T007), resources from variables (cores, memory), empty metadata placeholder for docker-compose (will be filled in US2)
- [x] T014 [US1] Wire output values in `terraform/outputs.tf` to reference actual resources: `ydb_endpoint` from T010, `registry_id` from T011, `vm_external_ip` and `vm_internal_ip` from T013, `service_account_id` from T007
- [x] T015 [US1] Validate with `terraform init && terraform plan` — all resources should plan successfully with no errors

**Checkpoint**: `terraform apply` creates YDB, registry, network, service account, and VM. `terraform destroy` removes all resources cleanly. Satisfies FR-001, FR-004, FR-008, FR-009, FR-010, SC-001, SC-004.

---

## Phase 4: User Story 2 — Run Example Applications on Container-Optimized VM (Priority: P2)

**Goal**: All three example applications are packaged as distroless containers and auto-started on the COI VM via Docker Compose metadata.

**Independent Test**: SSH into the provisioned VM, run `sudo docker ps`, verify all 3 containers are running. Inspect images to confirm distroless base (no shell available).

### Implementation for User Story 2

- [x] T016 [US2] Create parameterized `Dockerfile` at repository root — multi-stage build with `golang:1.26-alpine` builder, `CGO_ENABLED=0`, `-ldflags="-w -s"`, `gcr.io/distroless/static-debian12:nonroot` runtime, `ARG EXAMPLE` for selecting example directory
- [x] T017 [US2] Create Docker Compose specification in `terraform/docker-compose.yaml` — version 3.7, three services (01_db_producer, 02_cdc_worker, 03_topic), each referencing `cr.yandex/${registry_id}/<example>:latest`, restart policy `always`, environment variables `YDB_ENDPOINT` and `YDB_SA_KEY_FILE=/secrets/sa.json`, volume mount for SA key file
- [x] T018 [US2] Update compute instance in `terraform/compute.tf` to load `docker-compose.yaml` via `metadata.docker-compose = file("${path.module}/docker-compose.yaml")` and add `user-data` for cloud-init SSH key configuration
- [x] T019 [US2] Add Makefile targets for building and pushing container images — `docker-build` (builds all 3 images), `docker-push` (pushes all 3 to registry), `docker-login` (authenticates to `cr.yandex`) in `Makefile`
- [x] T020 [US2] Verify Dockerfile builds locally for each example: `docker build --build-arg EXAMPLE=01_db_producer -t test-01 .` (repeat for 02, 03) — all must succeed

**Checkpoint**: Docker images build from distroless base (SC-005). VM metadata declares all 3 containers. After `terraform apply` + image push + VM restart, containers auto-start (FR-002, FR-003, FR-004, FR-005, SC-002).

---

## Phase 5: User Story 3 — Examples Connect to YDB Cluster (Priority: P3)

**Goal**: Example containers running on the VM successfully authenticate and operate against the provisioned YDB cluster.

**Independent Test**: SSH into VM, inspect container logs (`sudo docker logs <container>`), verify successful YDB connection and database operations.

### Implementation for User Story 3

- [x] T021 [US3] Configure SA key file provisioning on VM — add a `null_resource` or `file` provisioner in `terraform/compute.tf` to copy `sa.json` to the VM's `/etc/secrets/sa.json` path (or use cloud-init `write_files` in `user-data`), ensure the path matches the Docker Compose volume mount
- [x] T022 [US3] Update `terraform/docker-compose.yaml` to template the `YDB_ENDPOINT` value from Terraform's `yandex_ydb_database_serverless` output — use Terraform `templatefile()` function or a `.tftpl` template to inject the actual YDB endpoint into the compose file
- [x] T023 [US3] Ensure VM service account has `ydb.editor` role (verify T009 is correctly wired) and that the SA key file mounted into containers matches the service account that has YDB access
- [x] T024 [US3] Add goose migration step to quickstart workflow — update `Makefile` with a `deploy` target that runs `terraform apply`, `docker-build`, `docker-push`, `make migrate` in sequence, ensuring schema exists before containers start

**Checkpoint**: End-to-end flow works: `terraform apply` → build/push images → migrate → VM containers connect to YDB and perform read/write operations. Satisfies FR-006, FR-007, SC-003.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories and final validation

- [x] T025 [P] Add `.gitignore` entries for Terraform state files (`terraform/.terraform/`, `terraform/*.tfstate`, `terraform/*.tfstate.backup`, `terraform/.terraform.lock.hcl`, `terraform/terraform.tfvars`)
- [x] T026 [P] Add `sa.json` to `.gitignore` if not already present (prevent credential leaks)
- [ ] T027 Validate full end-to-end workflow per `quickstart.md` — `terraform init` → `terraform apply` → docker build/push → `make migrate` → verify containers running and connected to YDB
- [x] T028 Run `terraform fmt` to ensure all HCL files are properly formatted
- [x] T029 Run `terraform validate` to confirm configuration is syntactically valid
- [ ] T030 Verify `terraform destroy` removes 100% of resources with no orphans (SC-004)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on T001, T002 from Setup (provider + variables must exist)
- **User Story 1 (Phase 3)**: Depends on Phase 2 completion (network, subnet, SA, IAM roles)
- **User Story 2 (Phase 4)**: Depends on Phase 3 (compute instance must exist to add metadata)
- **User Story 3 (Phase 5)**: Depends on Phase 4 (Docker Compose and images must exist for YDB connectivity testing)
- **Polish (Phase 6)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Phase 2 — creates the infrastructure skeleton
- **User Story 2 (P2)**: Depends on US1 — needs the compute instance and registry to exist before adding container metadata and building images
- **User Story 3 (P3)**: Depends on US2 — needs containers running before verifying YDB connectivity

> Note: Unlike typical application features, these stories are **sequentially dependent** because each builds on the infrastructure created by the previous story. This is inherent to infrastructure provisioning — you cannot run containers on a VM that doesn't exist yet.

### Within Each User Story

- Terraform resources before metadata/configuration
- Independent resources (marked [P]) can be created in parallel
- Wire outputs after all referenced resources exist
- Validate with `terraform plan` before moving to next story

### Parallel Opportunities

- **Phase 1**: T003 and T004 can run in parallel (different files)
- **Phase 2**: T007 can run in parallel with T005/T006 (different files)
- **Phase 3**: T010 and T011 can run in parallel (independent resources in different files)
- **Phase 6**: T025 and T026 can run in parallel (different files)

---

## Parallel Example: User Story 1

```text
# These two resources are independent and can be created simultaneously:
Task: "Create YDB Serverless database in terraform/ydb.tf" (T010)
Task: "Create container registry in terraform/registry.tf" (T011)

# Then these depend on the above:
Task: "Create compute instance in terraform/compute.tf" (T012 → T013)
Task: "Wire output values in terraform/outputs.tf" (T014)
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001–T004)
2. Complete Phase 2: Foundational (T005–T009)
3. Complete Phase 3: User Story 1 (T010–T015)
4. **STOP and VALIDATE**: Run `terraform apply` in a clean folder, verify all resources in console
5. Infrastructure skeleton is deployable

### Incremental Delivery

1. Setup + Foundational → Terraform project structure ready
2. User Story 1 → Infrastructure provisions/destroys cleanly (MVP!)
3. User Story 2 → Containers packaged and running on VM
4. User Story 3 → End-to-end connectivity to YDB verified
5. Polish → gitignore, formatting, full quickstart validation

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- User stories are sequentially dependent (infrastructure layering)
- No automated tests — validation is manual per constitution
- Commit after each task or logical group
- Stop at any checkpoint to validate independently
- `terraform plan` is your dry-run validation at every checkpoint
