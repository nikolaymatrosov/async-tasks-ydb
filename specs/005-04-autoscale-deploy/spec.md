# Feature Specification: Load-Test & Autoscaling Deployment for Example 04

**Feature Branch**: `005-04-autoscale-deploy`
**Created**: 2026-04-22
**Status**: Draft
**Input**: Deploy example 04 (coordinated-table-workers) to an autoscaling instance group, verify it handles 10M events/day (115 RPS), and report metrics to Yandex Monitoring via Unified Agent.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Validate Sustained Throughput (Priority: P1)

An operator runs a load test against the deployed example 04 cluster and confirms it sustains 115 requests per second (10M events/day) without task loss, excessive error rates, or runaway latency over a meaningful test window.

**Why this priority**: The primary goal is a capacity proof. Without confirming the throughput target, the rest of the deployment has no success signal.

**Independent Test**: Deploy at least one instance, send a sustained 115 RPS workload for 30 minutes, observe that processed-event count matches input and error rate stays below threshold. Delivers a pass/fail capacity verdict as a standalone result.

**Acceptance Scenarios**:

1. **Given** the cluster is running with at least one instance, **When** the load generator sends 115 RPS for 30 minutes, **Then** processed event count equals sent event count (±1% tolerance) and no tasks are permanently lost.
2. **Given** the cluster is under 115 RPS load, **When** any single instance fails and is replaced, **Then** throughput is restored within 60 seconds and no events are permanently dropped.
3. **Given** the load is 0 RPS, **When** the load generator ramps to 115 RPS within 10 seconds, **Then** the system stabilises processing rate within 30 seconds.

---

### User Story 2 - Autoscale on CPU Pressure (Priority: P2)

An operator can push load above the baseline throughput and observe the instance group automatically adding instances to keep CPU utilisation from becoming a sustained bottleneck, then scale back down when load subsides.

**Why this priority**: Without autoscaling validation, the deployment cannot handle traffic spikes and requires manual intervention, defeating the purpose of an elastic deployment.

**Independent Test**: Apply 3× baseline load for 10 minutes and confirm the instance count increases; reduce load to zero for 10 minutes and confirm the count decreases. Delivers elastic-scaling proof without requiring the throughput test to pass first.

**Acceptance Scenarios**:

1. **Given** the instance group is running at minimum size, **When** sustained CPU utilisation exceeds the scale-out threshold for the configured evaluation period, **Then** a new instance is added automatically within 5 minutes.
2. **Given** the instance group has scaled out, **When** CPU utilisation drops below the scale-in threshold for the configured evaluation period, **Then** the group scales back to minimum size within 10 minutes.
3. **Given** autoscaling is active, **When** a scale-out event occurs, **Then** the new instance becomes healthy and begins processing tasks before the next autoscaling evaluation window.

---

### User Story 3 - Real-Time Metrics in Yandex Monitoring (Priority: P3)

An operator can open Yandex Monitoring and view a dashboard showing per-instance and aggregate metrics — including event throughput, processing latency, error rate, and CPU utilisation — updated in near real-time.

**Why this priority**: Observability is essential for interpreting the load test results and for ongoing operational confidence. Without it, operators cannot distinguish a healthy cluster from a silently degraded one.

**Independent Test**: Deploy a single instance, apply any non-zero load, and verify that the expected metrics appear in Yandex Monitoring within two collection intervals. Delivers working telemetry pipeline as a standalone proof.

**Acceptance Scenarios**:

1. **Given** an instance is running and processing tasks, **When** 30 seconds have elapsed, **Then** throughput, error-rate, and CPU metrics for that instance appear in Yandex Monitoring.
2. **Given** metrics are flowing, **When** an instance is stopped, **Then** its metrics cease updating within two collection intervals and no stale data is reported for longer than that.
3. **Given** the instance group scales out, **When** the new instance starts, **Then** its metrics appear in Yandex Monitoring without any manual reconfiguration.

---

### Edge Cases

- What happens when all instances are at maximum CPU and autoscaling has already reached the group size limit?
- How does the system behave when the metadata service is temporarily unavailable at instance startup (container configuration cannot be retrieved)?
- What happens to in-flight tasks when an instance is terminated during a scale-in event?
- How are metrics handled when the Unified Agent container itself is unhealthy or has not yet started?
- What happens if the load generator sends bursts that significantly exceed 115 RPS for short intervals?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The deployment MUST run example 04 workers inside containers on each VM instance, with the container definition supplied via VM instance metadata (no pre-baked images containing runtime configuration).
- **FR-002**: Each VM instance MUST run a sidecar monitoring agent container alongside the application container, configured to collect and forward metrics without requiring SSH access or manual intervention.
- **FR-003**: The instance group MUST automatically add instances when average CPU utilisation exceeds a configurable threshold, and remove instances when utilisation drops below a configurable lower threshold.
- **FR-004**: The monitoring agent MUST collect both application-level metrics (events processed, errors, processing latency) and host-level resource metrics (CPU, memory, network I/O) from each instance.
- **FR-005**: All collected metrics MUST be forwarded to Yandex Monitoring and visible within two collection intervals of being generated, using instance-identity labels so per-instance and aggregate views are both possible.
- **FR-006**: The system MUST sustain a throughput of at least 115 events per second (10 million events per day) across the instance group with an end-to-end task loss rate below 1%.
- **FR-007**: The container configuration (docker-compose definition) MUST be retrievable from VM instance metadata at boot time, so replacing or adding instances requires no out-of-band configuration steps.
- **FR-008**: The monitoring agent MUST authenticate to Yandex Monitoring using the VM instance's attached service account, with no credentials stored on disk or in metadata.
- **FR-009**: A new instance MUST become healthy and start processing tasks within 3 minutes of being added by the autoscaler.
- **FR-010**: The deployment MUST be reproducible: re-applying the infrastructure definition creates an identical environment without manual steps.

### Key Entities

- **Instance Group**: The set of homogeneous VM instances running the workload; governs minimum/maximum size, autoscaling policy, and instance template.
- **Instance Template**: The blueprint for each VM — specifies machine type, disk, metadata payload (docker-compose definition), and attached service account.
- **Application Container**: The containerised example 04 worker process running on each instance, responsible for consuming and processing tasks.
- **Monitoring Agent Container**: The sidecar container running Unified Agent on each instance; collects metrics from the application and the host, then forwards them to Yandex Monitoring.
- **Autoscaling Policy**: The rules governing when the instance group grows or shrinks, based on CPU utilisation thresholds and evaluation periods.
- **Metrics Pipeline**: The end-to-end path from metric generation in the application → collection by the monitoring agent → delivery to Yandex Monitoring.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The cluster sustains 115 events per second for a continuous 30-minute window with fewer than 1% of events permanently lost or unprocessed.
- **SC-002**: When load drives CPU above the scale-out threshold, a new instance joins the group and begins processing within 5 minutes of the threshold being breached.
- **SC-003**: When load subsides below the scale-in threshold for the configured window, the group returns to minimum size within 10 minutes without manual intervention.
- **SC-004**: Metrics for every running instance appear in Yandex Monitoring within 30 seconds of those instances generating events, with no manual reconfiguration required when instances are added or removed.
- **SC-005**: A complete environment — from zero — can be reproduced by applying the infrastructure definition once, with all instances healthy and metrics flowing within 10 minutes of deployment completing.
- **SC-006**: Each new instance added by autoscaling reaches a healthy, task-processing state within 3 minutes of being created.

## Assumptions

- The YDB database and topics required by example 04 already exist and are accessible from the instance group's network; this feature does not provision them.
- The Yandex Cloud folder, VPC network, and subnet already exist; only instance-group-level resources and IAM bindings are in scope.
- The application container exposes metrics in Prometheus exposition format on a local port so the Unified Agent sidecar can scrape them via `metrics_pull`.
- The VM OS image supports running a container daemon and docker-compose via metadata on boot (Container-Optimized OS or equivalent).
- A service account with the necessary roles (Monitoring writer, YDB access) is created and attached to the instance template; secret management is out of scope.
- Load generation is performed by an external tool or script and is not part of this feature's deliverables — only the receiving infrastructure is in scope.
