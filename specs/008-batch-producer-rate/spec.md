# Feature Specification: Batch Producer Rate Control

**Feature Branch**: `008-batch-producer-rate`
**Created**: 2026-04-22
**Status**: Draft
**Input**: User description: "I want @04_coordinated_table/producer.go to batch workload to produce exactly given rate"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Precise Rate at Any Scale (Priority: P1)

An operator starts the producer with a target rate (e.g., 500 tasks/sec). The system groups task insertions into batches and submits them over timed windows so the actual throughput matches the configured rate within a tight tolerance — even at rates where per-item submission timing would be imprecise.

**Why this priority**: The core value of this feature. Without precise rate control, load tests and benchmarks yield unreliable data. This is the single capability that justifies the change.

**Independent Test**: Start producer with a fixed target rate. After a 30-second steady-state window, measure the number of tasks inserted. Pass if the observed rate is within ±5% of the target.

**Acceptance Scenarios**:

1. **Given** the producer is configured with a target rate of 100 tasks/sec, **When** it runs for 60 seconds, **Then** between 5,700 and 6,300 tasks are inserted (±5% tolerance).
2. **Given** the producer is configured with a target rate of 1,000 tasks/sec, **When** it runs for 60 seconds, **Then** between 57,000 and 63,000 tasks are inserted.
3. **Given** the producer is configured with a target of 1 task/sec, **When** it runs for 30 seconds, **Then** exactly 28–32 tasks are inserted.

---

### User Story 2 - Graceful Backpressure Handling (Priority: P2)

When the storage system is temporarily slow (high latency spikes), the producer absorbs the delay without exceeding its configured rate or accumulating an unbounded backlog of pending work.

**Why this priority**: Without this, a slow storage layer causes the producer to queue up unlimited work in memory, leading to crashes or bursts that far exceed the target rate once the storage recovers.

**Independent Test**: Introduce artificial delay to storage writes. Observe that the producer does not queue more than one batch worth of unsubmitted tasks and does not burst above the target rate on recovery.

**Acceptance Scenarios**:

1. **Given** storage latency rises above the batch window duration, **When** the producer is running, **Then** it waits for the current batch to complete before preparing the next one (no unbounded queuing).
2. **Given** storage recovers after a slowdown, **When** the producer resumes, **Then** the output rate returns to the target without a burst that overshoots the configured rate.

---

### User Story 3 - Accurate Rate Reporting (Priority: P3)

The producer logs its actual achieved throughput at regular intervals so an operator can confirm the observed rate matches the target without inspecting the database directly.

**Why this priority**: Observability is important for diagnosing rate drift, but the feature still delivers value even without it. Logging is a supporting concern.

**Independent Test**: Run the producer and inspect logs. Pass if throughput entries appear at regular intervals and report tasks-per-second values that match the observed insertion count.

**Acceptance Scenarios**:

1. **Given** the producer is running, **When** each reporting interval elapses, **Then** the log includes the number of tasks submitted in that interval and the computed rate.
2. **Given** a sustained slowdown causes the actual rate to drop below the target, **When** the reporting interval fires, **Then** the log clearly reflects the lower achieved rate (not the configured target).

---

### Edge Cases

- What happens when the configured rate is 1 task/sec or lower? (Single-item batches; rate control should still apply without errors.)
- What happens when the configured rate is very high (e.g., 10,000/sec)? (Batch sizes must remain bounded and memory usage must not grow unboundedly.)
- What happens when the producer starts and there is no connection to storage? (Startup should fail fast with a clear error; no silent rate drift.)
- What happens when the batch window expires but the previous batch has not yet completed? (The producer must not submit overlapping batches that could double the rate.)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The producer MUST group task insertions into batches sized to deliver the target rate over a configurable time window.
- **FR-002**: The actual insertion rate MUST stay within ±5% of the configured target during steady-state operation.
- **FR-003**: The producer MUST NOT queue more tasks than one batch window's worth of work at any time, preventing unbounded memory growth.
- **FR-004**: The producer MUST wait for the current batch to complete before dispatching a new one, avoiding concurrent overlapping submissions.
- **FR-005**: The producer MUST log the achieved throughput at regular intervals (default: every 5 seconds) showing tasks inserted and computed rate.
- **FR-006**: When the actual rate falls behind due to slow storage, the producer MUST NOT attempt to compensate with a burst that exceeds the target rate on recovery.
- **FR-007**: The producer MUST continue inserting tasks with the full set of existing attributes (id, hash, partition_id, priority, status, payload, created_at, and optionally scheduled_at) unchanged by this feature.
- **FR-008**: The ~10% scheduled-task behavior (future `scheduled_at` timestamps) MUST be preserved per task regardless of batch grouping.

### Key Entities

- **Batch**: A group of tasks assembled within a fixed time window and submitted together as a single unit of work. Key attributes: task count, window duration, target rate.
- **Time Window**: The fixed interval over which one batch is assembled and submitted. Determines how many tasks constitute one batch at a given rate.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Observed insertion rate is within ±5% of the configured target after a 30-second warm-up period, across all tested rates from 1 to 10,000 tasks/sec.
- **SC-002**: Memory used by pending (not-yet-submitted) tasks never exceeds the equivalent of one batch window's worth of tasks, regardless of storage latency.
- **SC-003**: Throughput logs appear at the configured reporting interval (default 5 seconds) with no gaps during normal operation.
- **SC-004**: On storage recovery after a slowdown, the measured rate returns to within ±5% of target within one reporting interval without a burst.

## Assumptions

- The batch window size (duration) is a tunable parameter; a reasonable default (e.g., 100ms) is chosen such that at common rates the batch size stays in the range of tens to hundreds of tasks.
- Storage can accept multi-row batch writes; the caller is responsible for keeping batch sizes within storage limits.
- Existing task attributes, distribution logic (partition hash, priority distribution), and the ~10% scheduled-task rate are all out of scope for this feature — they are preserved as-is.
- The ±5% rate tolerance accounts for timer imprecision and network jitter; tighter tolerances are a future concern.
