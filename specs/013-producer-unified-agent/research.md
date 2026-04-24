# Research: Unified Agent on Producer VMs

## Producer Prometheus metrics endpoint

**Decision**: The producer binary already exposes a Prometheus `/metrics` endpoint.

**Rationale**: `04_coordinated_table/cmd/producer/main.go:29` declares `--metrics-port` flag with default `9090` and starts `http.ListenAndServe` on that port. The docker-compose.yml.tpl just needs `--metrics-port 9090` added to the command and the port bound to `127.0.0.1:9090`.

**Alternatives considered**: System metrics only (no app metrics pull) — rejected because the binary already supports it at no extra cost.

---

## IAM permissions for monitoring.editor

**Decision**: No new IAM resources needed.

**Rationale**: `terraform/modules/db/iam.tf:18-22` already binds `monitoring.editor` to the `coi_vm` service account. The producer module receives this SA via `var.service_account_id` (passed as `module.db.service_account_id` in `main.tf`). Both workers and producer share the same `coi_vm` SA, so the role applies to both.

**Alternatives considered**: Separate SA for producer — rejected because the existing shared SA already has all required roles; adding a new SA would be unnecessary complexity.

---

## UA config template reuse

**Decision**: Copy `terraform/modules/workers/ua-config.yml.tpl` verbatim into `terraform/modules/producer/`.

**Rationale**: The config is parameterised only by `${folder_id}` and `${metrics_url}`. Both modules use the same monitoring API URL, the same buffer settings (100 MB), and the same `localhost:9090/metrics` scrape target. No module-specific changes are required.

**Alternatives considered**: Shared template in a parent module — rejected because it would introduce module coupling; the duplication is minimal and the modules may diverge in the future.

---

## user-data delivery

**Decision**: Use the same cloud-config `write_files` pattern as the workers module.

**Rationale**: COI VMs support cloud-init `user-data`. The workers module already uses this to write `/home/yc-user/ua-config.yml` at boot. The producer module's `compute.tf` currently has no `user-data` key in metadata — adding it follows the established pattern exactly.

**Alternatives considered**: Baking the config into the docker image — rejected because it would require rebuilding the UA image on config changes and prevents per-folder parameterisation.
