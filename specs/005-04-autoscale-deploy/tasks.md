# Tasks: Load-Test & Autoscaling Deployment for Example 04

**Input**: Design documents from `specs/005-04-autoscale-deploy/`
**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓

**Tests**: Not requested — no test tasks generated.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup

**Purpose**: Verify the existing codebase compiles cleanly before making any modifications

- [X] T001 Run `go vet ./04_coordinated_table/` to confirm the existing binary builds without errors before changes begin

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Go binary metric instrumentation and Terraform variable declarations — required before any user story can be deployed or tested

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T002 Add `tasksErrors int64` atomic field to the `Stats` struct in `04_coordinated_table/display.go` and update `printStats` to display it alongside the existing counters
- [X] T003 Increment `atomic.AddInt64(&s.tasksErrors, 1)` in `04_coordinated_table/worker.go` on each failed `acquireLock` call and on each failed `completeTask` call (depends on T002)
- [X] T004 [P] Create `04_coordinated_table/metrics.go` (`package main`) implementing `metricsHandler(s *Stats, workerID string) http.HandlerFunc` that writes Prometheus text exposition format (v0.0.4) with five metrics from the data-model contract: `coordinator_tasks_processed_total`, `coordinator_tasks_locked_total`, `coordinator_tasks_errors_total`, `coordinator_partitions_owned`, `coordinator_up`; all values read via `atomic.LoadInt64`; returns `404` for non-`/metrics` paths; no external dependencies beyond `net/http`
- [X] T005 Add `--metrics-port` int flag (default `9090`) to `04_coordinated_table/main.go`; generate `workerID` once in `main()` using the existing `uuid` package; call `http.ListenAndServe` (or a `startMetricsServer` wrapper) with `metricsHandler` before `runWorker`/`runProducer`; log startup with `slog.Info("metrics server started", "addr", ":9090")` (depends on T004)
- [X] T006 [P] Add new Terraform variables to `terraform/variables.tf`: `ydb_endpoint` (string, required), `ydb_database` (string, required), `ig_max_size` (number, default `5`), `ig_min_zone_size` (number, default `1`), `ig_cpu_target` (number, default `70`), `ig_stabilization_duration` (number, default `300`), `ig_warmup_duration` (number, default `120`), `ig_measurement_duration` (number, default `60`), `worker_rate` (number, default `115`)
- [X] T007 [P] Create `terraform/docker-compose.yml.tpl` — a `templatefile()`-compatible YAML template defining one `coordinator` service using image `${coordinator_image}`, environment vars `YDB_ENDPOINT=${ydb_endpoint}` and `YDB_DATABASE=${ydb_database}`, command flags `--mode worker --rate ${worker_rate} --metrics-port 9090`; expose port `9090` on the loopback interface; `restart: unless-stopped`

**Checkpoint**: Foundation ready — Go binary changes compile, Terraform variables declared

---

## Phase 3: User Story 1 — Validate Sustained Throughput (Priority: P1) 🎯 MVP

**Goal**: Deploy a working instance group with at least one instance; confirm it sustains 115 RPS for 30 minutes with < 1% task loss

**Independent Test**: Run `terraform apply`, wait for one instance to reach healthy state, start a producer at `--rate 115` for 30 minutes, verify that `coordinator_tasks_processed_total` in instance logs equals sent count (±1%) and `coordinator_tasks_errors_total` rate stays below 1.15/s

- [X] T008 [US1] Replace the existing `yandex_compute_instance.coi_vm` resource with `yandex_compute_instance_group.workers` in `terraform/compute.tf` — set `initial_size = 1`, attach the existing service account, use COI boot disk, set `metadata = { "docker-compose" = templatefile("${path.module}/docker-compose.yml.tpl", { coordinator_image = local.coordinator_image, ydb_endpoint = var.ydb_endpoint, ydb_database = var.ydb_database, worker_rate = var.worker_rate }) }`; do not add an `auto_scale` block yet (depends on T006, T007)
- [X] T009 [P] [US1] Update `terraform/outputs.tf` — remove the existing `instance_ip` output (referencing the deleted `yandex_compute_instance`) and add `instance_group_id = yandex_compute_instance_group.workers.id`
- [X] T010 [US1] Run `go vet ./04_coordinated_table/` to confirm all Go changes from Phase 2 compile without errors; fix any issues found before proceeding

**Checkpoint**: User Story 1 testable — `terraform apply` deploys one instance; run 30-min load test at 115 RPS to validate throughput

---

## Phase 4: User Story 2 — Autoscale on CPU Pressure (Priority: P2)

**Goal**: Add CPU-driven autoscale policy so the instance group grows under sustained high load and shrinks when load subsides

**Independent Test**: Apply `terraform apply` after T011, push 3× baseline load for 10 min, confirm `yc compute instance-group list-instances --id <group_id>` shows new instances; reduce load to zero for 10 min, confirm group returns to minimum size (≤ `ig_min_zone_size`)

- [X] T011 [US2] Add an `auto_scale` policy block inside `yandex_compute_instance_group.workers` in `terraform/compute.tf` with `min_zone_size = var.ig_min_zone_size`, `max_size = var.ig_max_size`, and a `cpu_utilization_rule { cpu_utilization_target = var.ig_cpu_target }` metric; also set `measurement_duration = var.ig_measurement_duration`, `stabilization_duration = var.ig_stabilization_duration`, `warmup_duration = var.ig_warmup_duration` (depends on T008)

**Checkpoint**: User Story 2 testable — elastic scaling observable under variable CPU load

---

## Phase 5: User Story 3 — Real-Time Metrics in Yandex Monitoring (Priority: P3)

**Goal**: Configure the Unified Agent sidecar and IAM so per-instance Prometheus metrics appear in Yandex Monitoring within 30 seconds of instance startup

**Independent Test**: Deploy a single instance (`terraform apply`), apply any non-zero load, query `coordinator_tasks_processed_total{worker_id=~".*"}` in Yandex Monitoring console — metrics must appear within ~30 seconds (two 15 s scrape intervals) without manual reconfiguration

- [X] T012 [P] [US3] Add a `yandex_resourcemanager_folder_iam_member` binding in `terraform/iam.tf` granting `monitoring.editor` to the `coi_vm` service account: `role = "monitoring.editor"`, `folder_id = var.folder_id`, `member = "serviceAccount:${yandex_iam_service_account.coi_vm.id}"` (same pattern as existing bindings in the file)
- [X] T013 [P] [US3] Create `terraform/ua-config.yml.tpl` — Unified Agent YAML config template with two routes: (1) `metrics_pull` input scraping `${metrics_url}` every 15 s, output to `yc_metrics` with `folder_id = "${folder_id}"`; (2) `linux_metrics` input with `/proc` mounted as `/ua_proc`, output to the same `yc_metrics` block; global auth block: `iam.cloud_meta: {}` (picks up SA IAM token from VM metadata service)
- [X] T014 [US3] Extend `terraform/docker-compose.yml.tpl` to add a `unified-agent` service: image `cr.yandex/yc/unified-agent:latest`, `network_mode: host` so `localhost:9090` resolves to the coordinator metrics port, bind-mount `/proc:/ua_proc:ro`; supply the rendered ua-config by adding a second instance metadata key `ua-config` in `terraform/compute.tf` (via `templatefile("${path.module}/ua-config.yml.tpl", { folder_id = var.folder_id, metrics_url = "http://localhost:9090/metrics" })`) and have the UA container read it from `/etc/yandex-unified-agent/config.yml` using a `user-data` cloud-init script that writes the metadata value to that path before containers start (depends on T007, T013)

**Checkpoint**: User Story 3 testable — all three stories independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final build gates and documentation housekeeping

- [X] T015 [P] Add or update `terraform/terraform.tfvars.example` to include all new required variables (`ydb_endpoint`, `ydb_database`, `folder_id`) with placeholder values matching the format shown in `specs/005-04-autoscale-deploy/quickstart.md`
- [X] T016 Run `go vet ./04_coordinated_table/` and `terraform validate` in `terraform/` as final build gates; resolve any errors before declaring the feature complete

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 completion — **BLOCKS all user stories**
- **User Story 1 (Phase 3)**: Depends on Phase 2 (T006, T007 for T008; T004+T005 for T010)
- **User Story 2 (Phase 4)**: Depends on T008 from Phase 3 (extends same Terraform resource)
- **User Story 3 (Phase 5)**: T012 and T013 depend on Phase 2 completion only (parallel with Phase 3/4); T014 depends on T007 and T013
- **Polish (Phase 6)**: Depends on all phases complete

### User Story Dependencies

- **US1 (P1)**: No dependency on US2 or US3 — basic instance group is standalone
- **US2 (P2)**: Extends the `yandex_compute_instance_group.workers` resource from US1 (T008 must complete first)
- **US3 (P3)**: T012 and T013 are independent of US1/US2; T014 extends T007 (docker-compose template)

### Within Each Phase

```
T002 → T003 (worker.go needs Stats.tasksErrors field from display.go)
T002 + T003 → T004 (metrics.go reads Stats fields including tasksErrors)
T004 → T005 (main.go starts the metrics server declared in metrics.go)
T006, T007 in parallel (independent files)
T008 depends on T006 + T007 (needs variables declared and template created)
T009, T010 in parallel after T008 and T005 (independent files)
T011 depends on T008 (extends same Terraform resource block)
T012, T013 in parallel (independent files — can start after Phase 2)
T014 depends on T007 + T013 (extends docker-compose template, needs ua-config template)
```

### Parallel Opportunities

- T004, T006, T007 can run in parallel (different files, no shared dependencies at this point)
- T009 and T010 can run in parallel within Phase 3
- T012 and T013 can run in parallel within Phase 5

---

## Parallel Example: Phase 2 Foundational

```bash
# After T001 completes and T002 completes, these three can run in parallel:
Task T003: Modify 04_coordinated_table/worker.go (increment tasksErrors)
Task T004: Create 04_coordinated_table/metrics.go (Prometheus handler)
Task T006: Update terraform/variables.tf (new variables)
Task T007: Create terraform/docker-compose.yml.tpl (COI metadata template)
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (Go metrics changes + Terraform vars)
3. Complete Phase 3: User Story 1 (basic instance group, no autoscale)
4. **STOP and VALIDATE**: `terraform apply` → deploy one instance → run 30-min 115 RPS load test → verify throughput
5. Proceed to US2 and US3 once MVP validated

### Incremental Delivery

1. Phase 1 + 2 → Binary builds, variables declared
2. Phase 3 → Deployable instance group → Validate US1 (throughput)
3. Phase 4 → Elastic scaling → Validate US2 (autoscale)
4. Phase 5 → Metrics pipeline → Validate US3 (observability)
5. Phase 6 → Final polish and build gate confirmation

---

## Notes

- [P] tasks involve different files with no blocking dependencies — safe to run concurrently
- [Story] label maps each task to a specific user story for traceability
- No new Go direct dependencies — `metrics.go` uses only `net/http` and `sync/atomic` from stdlib
- Build gate is `go vet ./04_coordinated_table/`, **not** `go build ./...` (per project convention)
- Terraform destructive change: applying T008 will destroy `yandex_compute_instance.coi_vm` — acceptable for a load-test environment; use `terraform state rm` first if the old VM must be preserved
