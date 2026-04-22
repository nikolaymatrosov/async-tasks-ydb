# Research: 005-04-autoscale-deploy

## Decision 1: App Metrics Exposure Strategy

**Decision**: Add a minimal Prometheus text-format HTTP `/metrics` endpoint directly to the `04_coordinated_table` Go binary using only stdlib `net/http`. No new `go.mod` direct dependencies.

**Rationale**: The `prometheus/client_golang` library is not currently in `go.mod` and the project constitution requires any new direct dependency to be justified. The Prometheus text exposition format is simple enough to emit manually for a small, fixed set of counters. Unified Agent's `metrics_pull` input accepts standard Prometheus format.

**Alternatives considered**:

- `prometheus/client_golang` — standard choice but adds a new direct dep and significant indirect deps; overkill for 4–5 counters.
- Logging-only (no HTTP) — incompatible with `metrics_pull`; would require a log-scraping pipeline which is more fragile.
- `expvar` + adapter — `expvar` does not emit Prometheus format directly; would still need a custom formatter.

**Implementation**: A new `metrics.go` file (same `package main`) starts a `net/http` server on `--metrics-port` (default `9090`). It exposes the existing atomic counters from `Stats` plus a `tasks_errors_total` counter from worker. The endpoint is registered before `runWorker`/`runProducer`.

---

## Decision 2: Instance Group with CPU Autoscaling

**Decision**: Replace `yandex_compute_instance` in `terraform/compute.tf` with `yandex_compute_instance_group` using `auto_scale` policy with CPU utilisation target.

**Rationale**: `yandex_compute_instance_group` is the Yandex Cloud resource that natively supports automatic scaling; single-instance resources have no equivalent. The CPU metric is the most directly correlated signal for this workload (CPU-bound transaction processing).

**Alternatives considered**:

- Manual scaling via external scripts — not reproducible or operator-friendly.
- Custom metrics-based scaling — possible but requires additional infrastructure; CPU is sufficient for this load test scenario.

**Key parameters**:

- `cpu_utilization_target`: 70 (scale out when average CPU > 70%)
- `measurement_duration`: 60s (averaging window)
- `warmup_duration`: 120s (ignore new instance metrics for 2 min post-start)
- `stabilization_duration`: 300s (wait 5 min before scale-in)
- `min_zone_size`: 1 (at least one instance always running)
- `max_size`: 5 (configurable via variable, default 5)
- `initial_size`: 1

---

## Decision 3: Docker-Compose via COI Metadata

**Decision**: Use the `docker-compose` metadata key on COI instances. The value is Terraform-rendered YAML containing both the app container and the Unified Agent sidecar. `templatefile()` injects the image URL, YDB endpoint, database path, and folder ID.

**Rationale**: COI's konlet daemon natively reads the `docker-compose` metadata key and runs `docker-compose up` at boot — this is the documented approach for multi-container COI deployments. Only one declaration type is allowed (either `docker-compose` or `docker-container-declaration`); `docker-compose` supports multi-container sidecar patterns.

**Alternatives considered**:

- `user-data` (cloud-init) with docker run commands — works but is less declarative; docker-compose is COI's native multi-container path.
- Baking config into the image — violates the separation of config and code; would require a new image build per config change.

**Metadata key**: exactly `docker-compose` (not `docker-compose.yaml`).

---

## Decision 4: Unified Agent Configuration

**Decision**: Run Unified Agent as a sidecar container (`cr.yandex/yc/unified-agent`) in the docker-compose. Config supplied via a bind-mounted file rendered from `terraform/ua-config.yml.tpl`. Two routes:

1. `metrics_pull` → scrape `http://localhost:9090/metrics` every 15s → `yc_metrics` output
2. `linux_metrics` → CPU/memory/network from `/proc` (mounted as `/ua_proc`) → `yc_metrics` output

Auth: `iam.cloud_meta: {}` — picks up the SA IAM token from the VM metadata service. No credentials on disk.

**Rationale**: `metrics_pull` is the standard pattern for scraping Prometheus endpoints. `linux_metrics` gives host-level visibility needed for the CPU autoscaling signal correlation. `cloud_meta` auth is the correct approach when running on a Yandex Cloud VM with an attached service account.

**Alternatives considered**:

- Unified Agent installed as a systemd service (not in a container) — harder to manage in a docker-compose-only setup; sidecar container is consistent with the rest of the deployment.
- Pushing metrics from the app directly — requires a YDB Monitoring SDK or direct Monitoring API calls; more coupling.

---

## Decision 5: IAM Roles

**Decision**: Add `monitoring.editor` role to the existing `coi_vm` service account (in addition to existing `container-registry.images.puller` and `ydb.editor`).

**Rationale**: `monitoring.editor` is the minimum role required for Unified Agent to write metrics to Yandex Monitoring via `yc_metrics`. The VM's service account is already attached to the instance and used for YDB auth (`yc.WithMetadataCredentials()`), so adding this role is the minimal change.

**Alternatives considered**:

- `monitoring.admin` — too broad; editor suffices for metric writes.
- Separate service account for UA — cleaner in theory but adds resource overhead for this load-test scenario.
