# Implementation Plan: Unified Agent on Producer VMs

**Branch**: `013-producer-unified-agent` | **Date**: 2026-04-24 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/013-producer-unified-agent/spec.md`

## Summary

Add Yandex Unified Agent as a sidecar to the producer instance group so that both system-level and application-level metrics (already exposed by the producer binary on port 9090) are forwarded to Yandex Monitoring. The implementation mirrors the pattern already in use in the workers module: a `ua-config.yml.tpl` written via cloud-config `user-data`, and a `unified-agent` service in `docker-compose.yml.tpl`. No IAM changes are needed — the shared `coi_vm` service account already has `monitoring.editor`.

## Technical Context

**Language/Version**: HCL (Terraform ≥ 1.5)
**Primary Dependencies**: `yandex-cloud/yandex` provider (already in `.terraform.lock.hcl`); `cr.yandex/yc/unified-agent:latest` container image
**Storage**: N/A (no schema changes)
**Testing**: `terraform plan` + manual verification in Yandex Monitoring console after `terraform apply`
**Target Platform**: Yandex Cloud COI VMs (Linux, amd64)
**Project Type**: Infrastructure-as-code (Terraform module)
**Performance Goals**: Metrics visible in Yandex Monitoring within 60 s of VM boot; 15 s scrape interval
**Constraints**: No new IAM resources; no new direct dependencies; producer module must stay structurally parallel to the workers module
**Scale/Scope**: 1 fixed-size producer VM (default); same UA config as workers

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ❌ N/A — this feature adds no Go example code |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ❌ N/A — no Go code introduced |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ❌ N/A — no schema changes |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ❌ N/A — UA config uses VM metadata IAM, no creds in Terraform |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ❌ N/A — no Go code introduced |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ❌ N/A — Terraform-only change |

## Complexity Tracking

| Violation                              | Why Needed                                                                        | Simpler Alternative Rejected Because                                                               |
|----------------------------------------|-----------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------|
| All constitution principles marked N/A | Feature is Terraform infrastructure only; no Go example code is added or modified | The constitution governs Go examples; a Terraform-only change has no applicable principles to verify |

## Project Structure

### Documentation (this feature)

```text
specs/013-producer-unified-agent/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output (N/A — no data model)
└── tasks.md             # Phase 2 output (/speckit-tasks)
```

### Source Code Changes

```text
terraform/modules/producer/
├── compute.tf              # modified — add user-data with ua-config cloud-config write
├── docker-compose.yml.tpl  # modified — add unified-agent service; add --metrics-port to producer
└── ua-config.yml.tpl       # new — copy of workers/ua-config.yml.tpl (identical parameterisation)
```

No other files change.

## Implementation Tasks

### Task 1 — Create `ua-config.yml.tpl` in the producer module

**File**: `terraform/modules/producer/ua-config.yml.tpl`
**Action**: Create — exact copy of `terraform/modules/workers/ua-config.yml.tpl`

The template is parameterised by `${folder_id}` and `${metrics_url}`. Both resolve identically for the producer module (`localhost:9090/metrics`). No content changes are required.

---

### Task 2 — Update `docker-compose.yml.tpl` in the producer module

**File**: `terraform/modules/producer/docker-compose.yml.tpl`

Two changes:

1. Add `--metrics-port 9090` to the `coordinator` service command and bind the port locally:

```yaml
    command:
      - "--rate"
      - "${producer_rate}"
      - "--metrics-port"
      - "9090"
    ports:
      - "127.0.0.1:9090:9090"
```

1. Add the `unified-agent` service (identical to workers module):

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

`folder_id` must be added to the `templatefile()` call in `compute.tf` (see Task 3).

---

### Task 3 — Update `compute.tf` in the producer module

**File**: `terraform/modules/producer/compute.tf`

Two changes:

1. Add `folder_id = var.folder_id` to the existing `templatefile()` call for `docker-compose`:

```hcl
"docker-compose" = templatefile("${path.module}/docker-compose.yml.tpl", {
  coordinator_image = local.coordinator_image
  ydb_endpoint      = var.ydb_endpoint
  ydb_database      = var.ydb_database
  producer_rate     = var.producer_rate
  apigw_url         = var.apigw_url
  folder_id         = var.folder_id
})
```

1. Add a `user-data` key to the metadata `merge()` call, using the same cloud-config `write_files` pattern as the workers module:

```hcl
"user-data" = <<-EOT
  #cloud-config
  ${var.ssh_public_key != "" ? "users:\n  - name: yc-user\n    sudo: ALL=(ALL) NOPASSWD:ALL\n    ssh_authorized_keys:\n      - ${var.ssh_public_key}" : ""}
  write_files:
    - path: /home/yc-user/ua-config.yml
      permissions: '0644'
      content: |
        ${indent(4, templatefile("${path.module}/ua-config.yml.tpl", {
  metrics_url = "http://localhost:9090/metrics"
  folder_id   = var.folder_id
}))}
  EOT
```

The `merge()` in the metadata block already handles conditional SSH keys; `user-data` is added as a third key in that merge.

---

### Task 4 — Validate with `terraform plan`

Run `terraform plan` from the `terraform/` directory and verify:

- No unexpected resource replacements (producer instance group should show an in-place update to metadata, not recreation)
- No errors in template rendering

---

### Task 5 — Verify metrics in Yandex Monitoring

After `terraform apply`:

1. Wait ~60 s for the producer VM to boot and UA to start
1. Open Yandex Monitoring → Metrics Explorer
1. Confirm `sys.*` metrics (CPU, memory, network) appear for the `async-tasks-producer` instance group
1. Confirm `custom.*` or producer-namespaced metrics appear from the `localhost:9090/metrics` scrape

## Risks & Mitigations

| Risk | Likelihood | Mitigation |
| --- | --- | --- |
| Producer instance group is recreated instead of updated | Low | COI metadata updates are in-place for instance groups; confirm with `terraform plan` before apply |
| `--metrics-port` flag not wired in the producer binary | Resolved | Confirmed in `04_coordinated_table/cmd/producer/main.go:29` — flag exists with default 9090 |
| UA container can't reach `localhost:9090` | Low | UA uses `network_mode: host`, same as in workers — host networking makes `localhost` resolve correctly |
