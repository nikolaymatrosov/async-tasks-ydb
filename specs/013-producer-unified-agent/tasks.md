---

description: "Task list for 013-producer-unified-agent"
---

# Tasks: Unified Agent on Producer VMs

**Input**: Design documents from `/specs/013-producer-unified-agent/`
**Prerequisites**: plan.md, spec.md, research.md

**Organization**: Tasks are grouped by user story to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2)

## Path Conventions

All source changes are under `terraform/modules/producer/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: No new project initialization required — changes are added to the existing `terraform/modules/producer/` module.

*(No setup tasks needed — module already exists)*

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Create the UA config template that both user stories depend on.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T001 Create `terraform/modules/producer/ua-config.yml.tpl` as an exact copy of `terraform/modules/workers/ua-config.yml.tpl` (parameterised by `${folder_id}` and `${metrics_url}`)

**Checkpoint**: `ua-config.yml.tpl` exists in the producer module — user story work can now begin.

---

## Phase 3: User Story 1 — View producer system metrics in Yandex Monitoring (Priority: P1) 🎯 MVP

**Goal**: Unified Agent runs as a sidecar on producer VMs and forwards system-level metrics (CPU, memory, network, disk) to Yandex Monitoring automatically after `terraform apply`.

**Independent Test**: After `terraform apply`, open Yandex Monitoring and confirm `sys.*` metrics (CPU, memory, network, storage) appear for each producer VM within 60 seconds of boot — no manual steps required.

### Implementation for User Story 1

- [X] T002 [US1] Add the `unified-agent` sidecar service to `terraform/modules/producer/docker-compose.yml.tpl`:
  ```yaml
    unified-agent:
      image: cr.yandex/yc/unified-agent:latest
      network_mode: host
      entrypoint: ""
      environment:
        PROC_DIRECTORY: /ua_proc
        FOLDER_ID: ${folder_id}
      volumes:
        - /proc:/ua_proc:ro
        - /home/yc-user/ua-config.yml:/etc/yandex/unified_agent/config.yml
      restart: unless-stopped
  ```
- [X] T003 [US1] Update `terraform/modules/producer/compute.tf` with two changes:
  1. Add `folder_id = var.folder_id` to the existing `templatefile()` call for `docker-compose`
  2. Add a `"user-data"` key to the metadata `merge()` call using the cloud-config `write_files` pattern to write `/home/yc-user/ua-config.yml` at boot (mirror the workers module pattern, rendering `ua-config.yml.tpl` with `metrics_url = "http://localhost:9090/metrics"` and `folder_id = var.folder_id`)

**Checkpoint**: At this point, Unified Agent runs on producer VMs and system metrics are visible in Yandex Monitoring.

---

## Phase 4: User Story 2 — View producer application metrics in Yandex Monitoring (Priority: P2)

**Goal**: Application metrics exposed by the producer service on port 9090 are scraped by Unified Agent and forwarded to Yandex Monitoring every 15 seconds.

**Independent Test**: After `terraform apply`, confirm application metrics from the producer service appear in Yandex Monitoring with data points updating at least every 15 seconds.

### Implementation for User Story 2

- [X] T004 [US2] Add `--metrics-port 9090` to the `coordinator` service command and bind the port in `terraform/modules/producer/docker-compose.yml.tpl`:
  ```yaml
      command:
        - "--rate"
        - "${producer_rate}"
        - "--metrics-port"
        - "9090"
      ports:
        - "127.0.0.1:9090:9090"
  ```

**Checkpoint**: Producer application metrics appear in Yandex Monitoring alongside system metrics.

---

## Phase 5: Polish & Validation

**Purpose**: Validate correctness of all changes before apply.

- [X] T005 [P] Run `terraform plan` from the `terraform/` directory and verify: no unexpected resource replacements (producer instance group shows in-place metadata update, not recreation), no template rendering errors
- [ ] T006 (Manual post-apply) Verify in Yandex Monitoring that `sys.*` metrics and producer application metrics appear for producer VM instances within 60 s of boot

---

## Dependencies & Execution Order

### Phase Dependencies

- **Foundational (Phase 2)**: No dependencies — start immediately
- **User Story 1 (Phase 3)**: Depends on T001 (ua-config.yml.tpl must exist before compute.tf can reference it)
- **User Story 2 (Phase 4)**: Can start independently after Phase 2; modifies a different part of `docker-compose.yml.tpl` than T002 — coordinate if working in parallel
- **Polish (Phase 5)**: Depends on all previous phases complete

### User Story Dependencies

- **US1 (P1)**: Depends on Foundational (T001). No dependency on US2.
- **US2 (P2)**: Depends on Foundational (T001). No dependency on US1 — but T004 edits the same file as T002; complete T002 first to avoid merge conflicts.

### Within Each User Story

- T001 → T002, T003 (T001 must exist before compute.tf can use the template)
- T002 and T003 can be done in either order (different files)
- T004 edits `docker-compose.yml.tpl` — sequence after T002 to avoid conflicts

---

## Parallel Opportunities

```bash
# Phase 3 — T002 and T003 touch different files; run in parallel:
Task: "Add unified-agent service to docker-compose.yml.tpl" (T002)
Task: "Update compute.tf with folder_id and user-data" (T003)

# Phase 4 — T004 is independent; can start after T001:
Task: "Add --metrics-port 9090 to coordinator in docker-compose.yml.tpl" (T004)
# NOTE: serialize T004 after T002 if both are done in the same session to avoid file conflicts
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 2: T001 — create ua-config.yml.tpl
2. Complete Phase 3: T002 + T003 — add UA sidecar and user-data delivery
3. Validate: T005 — terraform plan
4. **STOP and VALIDATE**: apply and confirm system metrics in Yandex Monitoring
5. Proceed to US2 only if US1 is confirmed working

### Incremental Delivery

1. T001 → foundation ready
2. T002 + T003 → system metrics (US1 MVP)
3. T004 → application metrics (US2 increment)
4. T005 + T006 → validation

---

## Notes

- [P] tasks = different files, no sequential dependency
- [Story] label maps task to specific user story for traceability
- T002 and T004 both modify `docker-compose.yml.tpl`; complete T002 before T004 to avoid conflicts
- No IAM changes needed — `coi_vm` SA already has `monitoring.editor`
- No Go code changes — all tasks are HCL/YAML template changes
- Commit after each task or logical group
