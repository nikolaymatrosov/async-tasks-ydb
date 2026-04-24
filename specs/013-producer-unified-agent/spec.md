# Feature Specification: Unified Agent on Producer VMs

**Feature Branch**: `013-producer-unified-agent`
**Created**: 2026-04-24
**Status**: Draft
**Input**: User description: "add unified agent to producer VMs so I can monitor metrics from there"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View producer system metrics in Yandex Monitoring (Priority: P1)

An operator opens Yandex Monitoring and can see CPU, memory, network, and disk metrics for each producer VM without any manual setup after infrastructure is deployed.

**Why this priority**: System-level observability is the baseline requirement. Without it, operators have no visibility into whether producer VMs are healthy or overloaded.

**Independent Test**: After `terraform apply` completes, navigate to Yandex Monitoring and confirm system metrics (CPU, memory, network, storage) appear under the project folder for each producer VM instance.

**Acceptance Scenarios**:

1. **Given** producer VMs are running after a fresh `terraform apply`, **When** an operator opens Yandex Monitoring, **Then** CPU, memory, network, and disk metrics appear for each producer instance within 60 seconds of VM boot.
2. **Given** the producer instance group has 2 VMs, **When** metrics are viewed, **Then** each VM instance reports metrics independently, labeled by instance identifier.
3. **Given** a producer VM is terminated and replaced by auto-healing, **When** the replacement VM boots, **Then** metrics appear for the new instance automatically — no manual reconfiguration required.

---

### User Story 2 - View producer application metrics in Yandex Monitoring (Priority: P2)

An operator can see producer-specific application metrics (e.g., task injection rate, request counts) in Yandex Monitoring alongside system metrics.

**Why this priority**: Application metrics reveal producer behaviour beyond what system stats show — whether the producer is keeping up with its configured rate and whether requests are succeeding.

**Independent Test**: After `terraform apply`, confirm application metrics from the producer service appear in Yandex Monitoring under a distinct namespace, with data points updating every 15 seconds.

**Acceptance Scenarios**:

1. **Given** the producer service exposes application metrics, **When** Unified Agent is running on the same VM, **Then** those metrics are scraped and forwarded to Yandex Monitoring on a 15-second interval.
2. **Given** the producer service temporarily stops responding to metric scrape requests, **When** it recovers, **Then** metric collection resumes automatically without restarting the monitoring agent.
3. **Given** the producer is running at its configured task rate, **When** metrics are viewed, **Then** the observed application metrics reflect the actual configured rate.

---

### Edge Cases

- What happens if the producer VM cannot reach the Yandex Monitoring API (network partition)? Metrics must be buffered locally and forwarded once connectivity is restored, without data loss up to the buffer limit.
- What if the producer service is not yet ready when Unified Agent starts? Agent must retry metric collection without crashing, and begin reporting as soon as the service is available.
- What happens on `terraform apply` when only the UA config changes but no VM replacement occurs? The updated config must take effect on the next VM cycle or through a controlled update.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Each producer VM MUST run a Unified Agent process collecting and forwarding metrics to Yandex Monitoring.
- **FR-002**: Unified Agent MUST collect system-level metrics (CPU, memory, network, storage, I/O, kernel) from each producer VM.
- **FR-003**: Unified Agent MUST forward all collected metrics to Yandex Monitoring using the VM's attached service account IAM token — no static credentials.
- **FR-004**: Unified Agent MUST collect and forward application-level metrics from the producer service on the same VM.
- **FR-005**: Metrics MUST be attributed to the correct Yandex Cloud folder so they appear under the project in Yandex Monitoring.
- **FR-006**: The Unified Agent configuration MUST be applied at VM boot time as part of the existing infrastructure-as-code deployment — no manual steps after `terraform apply`.
- **FR-007**: Metric collection MUST use a local buffer to tolerate temporary network interruptions without losing data up to the configured buffer capacity.
- **FR-008**: The producer module's Unified Agent setup MUST follow the same pattern already used in the workers module to maintain consistency.

### Key Entities

- **Producer VM** (`yandex_compute_instance_group.producer`): The set of fixed-size VMs running the task-injection service; receives the Unified Agent sidecar.
- **Unified Agent** (sidecar container): The Yandex Monitoring collection agent; collects system and application metrics and ships them to the cloud monitoring API.
- **Yandex Monitoring**: The cloud-hosted metrics store and dashboard; receives metrics forwarded by Unified Agent using the VM's IAM identity.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: System metrics (CPU, memory, network, disk) for all producer VMs appear in Yandex Monitoring within 60 seconds of VM boot, with no manual configuration.
- **SC-002**: Application metrics from the producer service appear in Yandex Monitoring and update at least every 15 seconds under normal operating conditions.
- **SC-003**: Metric collection resumes automatically after a transient network interruption of up to 5 minutes, with no data gaps beyond the interruption window.
- **SC-004**: A `terraform apply` from scratch (no prior state) results in fully functioning metric collection on all producer VMs without any additional operator action.
- **SC-005**: Metric collection does not require any credentials to be stored in Terraform state, environment variables, or on-disk key files.

## Assumptions

- The producer VM's existing service account already has sufficient permissions to write to Yandex Monitoring (same `monitoring.editor` role used by the workers module); if not, a role binding must be added.
- The producer service exposes application metrics in Prometheus exposition format on a local port, mirroring the pattern used by the coordinator worker service.
- The `ua-config.yml.tpl` template from the workers module can be reused as-is or with minor changes for the producer module.
- VM boot disk size is sufficient to hold the Unified Agent buffer (100 MB as configured in the workers module).
