# Feature Specification: Per-Entity Ordered Task Delivery

**Feature Branch**: `017-entity-task-ordering`
**Created**: 2026-04-29
**Status**: Draft
**Input**: User description: "I want to maintain order of the coordinated tasks related to the given entity. For example for entity E1 events A, B, C should be delivered in this order. If we have problems delivering event A and postponed it for any backoff period this means that we MUST NOT handle events B and C while A is not successfully delivered."

## Clarifications

### Session 2026-04-29

- Q: Where should this feature live in the repo? → A: A new self-contained example directory `05_ordered_tasks/`, forked from (not modifying) the existing `04_coordinated_table/`.
- Q: Should the example carry a `priority` column on the task row? → A: No. `priority` is dropped from the schema in this fork to simplify the demonstration; per-entity ordering is the only ordering signal.
- Q: How is the per-entity ordinal materialised? → A: Producer-generated synthetic monotonic value. The producer is a single-instance process; on generating each new entity_id, it picks a fake starting offset and increments it for each subsequent message produced for that entity within the same generation window, then forgets the counter immediately after the rows are written to the database. The ordinal is opaque to consumers — only its strict monotonicity per entity_id matters. No topic, no relay, and no read-modify-write transaction.
- Q: What is the producer concurrency model? → A: Exactly one producer process at a time. The spec's earlier edge case "two producers concurrently submitting for the same entity" is therefore out of scope for this fork (see Edge Cases section update).
- Q: How is the table's `partition_id` assigned? → A: `partition_id = murmur3.Sum32([]byte(entity_id)) % partitions` — hashed by `entity_id` (not by task id), so all tasks for a given entity share one partition and are dispatched by a single worker at a time.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Strict in-order delivery per entity (Priority: P1)

A producer submits a sequence of related events for a given entity (e.g., for entity `E1`: `A`, then `B`, then `C`). Downstream consumers must observe and process those events in the exact submission order. No event for an entity can be handed to a consumer until its predecessor for the same entity has reached a terminal success state.

**Why this priority**: This is the core guarantee the feature exists to deliver. Without it, downstream systems can apply state changes out of order (e.g., "update profile" applied before "create profile") leading to data corruption or business-logic violations. Every other behavior in this spec is a refinement of this guarantee.

**Independent Test**: Submit three tasks (`A`, `B`, `C`) for the same entity, with all consumers healthy. Verify that `B` is not dispatched until `A` is acknowledged successful, and `C` is not dispatched until `B` is acknowledged successful — even when multiple consumers are available and idle.

**Acceptance Scenarios**:

1. **Given** entity `E1` has three pending tasks `A`, `B`, `C` enqueued in that order and consumers are idle, **When** the system dispatches work, **Then** only `A` is delivered first; `B` and `C` remain pending until `A` completes successfully.
2. **Given** task `A` for entity `E1` has just completed successfully, **When** the system dispatches the next batch, **Then** `B` becomes eligible for delivery and `C` remains pending until `B` completes.
3. **Given** two distinct entities `E1` and `E2` each with multiple pending tasks, **When** the system dispatches work, **Then** the head task of `E1` and the head task of `E2` may be delivered concurrently (cross-entity parallelism is preserved).

---

### User Story 2 - Backoff of a head task blocks the entity's queue (Priority: P1)

When a consumer fails to handle the head task for an entity and the system schedules a retry after a backoff delay, every later task for that same entity must remain undelivered for the full backoff window — even if consumers are available. Only when the head task eventually reaches terminal success may the next task for that entity be released.

**Why this priority**: This is the explicit constraint the user called out. Releasing later tasks during the backoff would defeat the ordering guarantee, because a successful retry of `A` happening after `B` was already delivered would invert the observed order.

**Independent Test**: Submit `A`, `B`, `C` for entity `E1`. Force `A` to fail and enter a backoff. During the backoff window, verify that `B` and `C` are not dispatched to any consumer. After `A` eventually succeeds, verify `B` and then `C` are dispatched in order.

**Acceptance Scenarios**:

1. **Given** task `A` for entity `E1` has failed and is in a backoff/retry-pending state, **When** any number of consumers poll for work, **Then** no subsequent task for entity `E1` (`B`, `C`, …) is delivered to any consumer.
2. **Given** task `A` for `E1` is in backoff and tasks for entity `E2` are pending, **When** consumers poll, **Then** tasks for `E2` continue to be delivered normally; only `E1`'s queue is blocked.
3. **Given** task `A` for `E1` has been retried and ultimately succeeded, **When** the next dispatch cycle runs, **Then** `B` becomes the new head task and is eligible for immediate delivery.

---

### User Story 3 - Permanent failure handling for an entity's head task (Priority: P2)

A head task may exhaust its retry policy and reach a terminal failure state (poison message). The system must have a defined, observable behavior so that the entity's downstream queue does not silently stall forever, while still upholding the "no out-of-order delivery" guarantee.

**Why this priority**: Without this, a single permanently-failing task can indefinitely block all future work for an entity, which becomes an operational outage. This is critical for production but secondary to the core ordering mechanic.

**Independent Test**: Submit `A`, `B`, `C` for `E1`. Drive `A` to its terminal-failure state. Verify the configured behavior takes effect (the entity is quarantined and operators are notified; `B` and `C` are not silently delivered out of order).

**Acceptance Scenarios**:

1. **Given** task `A` for `E1` has reached terminal failure (retries exhausted), **When** dispatch runs, **Then** `B` and `C` are NOT released as if `A` had succeeded; the entity's queue is marked as blocked and surfaced to operators.
2. **Given** an operator has explicitly resolved or skipped the failed head task `A` (per the system's documented intervention path), **When** the next dispatch runs, **Then** `B` becomes eligible for delivery.

---

### User Story 4 - Test target HTTP server with order validation and fault injection (Priority: P1)

To validate the ordering guarantee end-to-end, the system needs a dedicated test target HTTP server that replaces the production HTTP destination (currently API Gateway). The target server (a) verifies that events for each entity arrive in submission order and surfaces any violation as both a structured log entry and a metric, and (b) can be configured to deterministically respond with retryable error statuses (HTTP 429 or 5xx) for a configurable percentage of incoming requests, giving workers a real reason to invoke their backoff/retry path.

**Why this priority**: Without this target, the ordering guarantee from User Stories 1–3 cannot be exercised in an integration test against the real worker code path, and the backoff-blocks-the-queue behavior cannot be triggered without artificially crashing real downstream services. This is required infrastructure for verifying the feature works.

**Independent Test**: Run the target server with a configured fault rate of 30% and a known stream of ordered events for several entities. Verify (1) when events arrive in order, no violations are reported; (2) when fault injection causes retries, the worker eventually delivers the same event again and the order check still passes (deduplication on retry); (3) if the worker were to deliver events out of order for a given entity, the target reports the violation in logs and increments the violation metric.

**Acceptance Scenarios**:

1. **Given** the target server is running with fault injection disabled, **When** workers deliver events `A`, `B`, `C` for entity `E1` in submission order, **Then** the server records all three as accepted and emits zero ordering-violation log entries and zero increments on the violation metric.
2. **Given** the target server is configured to return HTTP 429 for 50% of requests, **When** a worker attempts delivery, **Then** approximately 50% of attempts (within statistical tolerance over a sufficient sample) receive a 429 response, and the worker's backoff path is exercised.
3. **Given** the target server is configured to return HTTP 503 for 25% of requests, **When** a worker attempts delivery, **Then** approximately 25% of attempts receive a 5xx response.
4. **Given** the target server has previously accepted event `B` for entity `E1`, **When** event `A` for the same entity (a lower per-entity ordinal) arrives afterwards, **Then** the server emits a structured log entry identifying the entity, the expected next ordinal, and the offending ordinal, AND increments an ordering-violation metric labeled by entity.
5. **Given** the target server receives a duplicate delivery of an already-accepted event for entity `E1` (same per-entity ordinal as the last accepted), **When** the request is processed, **Then** the duplicate is treated as idempotent (not flagged as a violation) and the response indicates success.
6. **Given** the target server is started with both order validation and fault injection configured, **When** it receives traffic, **Then** the operator can observe current configured fault rates, total accepted events per entity, and total ordering violations through the server's observability surface.

---

### Edge Cases

- **Concurrent submission for the same entity**: Out of scope for this fork. The producer is single-instance (see Clarifications). Multi-producer support is a future extension and would need a coordination mechanism beyond the producer's in-memory counter.
- **Consumer crash mid-processing**: A consumer picks up head task `A`, then crashes before reporting success or failure. Subsequent tasks for the entity must remain blocked until `A`'s outcome is reconciled (timeout-based reclaim → either retry-with-backoff or terminal-failure path).
- **Duplicate delivery attempts**: If at-least-once delivery is in effect and `A` is delivered twice, the ordering guarantee for `B` is unaffected — `B` is still gated on `A` reaching terminal success exactly once.
- **Empty entity queue**: When an entity has no pending tasks, it imposes no constraints on dispatch.
- **Very large per-entity backlog**: The ordering guarantee must hold whether an entity has 3 pending tasks or 30,000.
- **Entity identifier reuse**: If an entity's queue drains to empty and a new task arrives later, that new task is the head and is immediately eligible — there is no "memory" of past blocking.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST associate every coordinated task with exactly one entity identifier supplied by the producer.
- **FR-002**: The producer MUST assign each task a synthetic, strictly-increasing per-entity ordinal at generation time and persist it on the row at acceptance, such that for any two tasks of the same entity the relative order of their ordinals matches the producer's generation order. The ordinal need not be contiguous and is opaque to consumers.
- **FR-003**: System MUST guarantee that, for any given entity, at most one task is being processed by a consumer at any time (head-of-line serialization per entity).
- **FR-004**: System MUST NOT release a non-head task for an entity to any consumer while that entity's current head task is in any non-terminal state (in-flight, awaiting retry, in backoff, or unreconciled after consumer failure).
- **FR-005**: System MUST allow tasks belonging to different entities to be processed concurrently; per-entity ordering MUST NOT serialize the system as a whole.
- **FR-006**: When the head task for an entity fails transiently and is scheduled for retry after a backoff delay, System MUST keep all later tasks for the same entity undelivered for the entire backoff window, regardless of consumer availability.
- **FR-007**: When the head task for an entity completes with terminal success, System MUST make the next task for that entity (if any) eligible for delivery on the next dispatch decision.
- **FR-008**: When the head task for an entity reaches terminal failure (e.g., retries exhausted), System MUST NOT silently advance the entity's queue. The entity MUST be placed into a clearly identifiable blocked state and the condition MUST be observable to operators.
- **FR-009**: System MUST provide an operator-initiated path to resolve a terminally-failed head task (e.g., mark resolved, skip, or replay) so that the entity's queue can advance. The chosen resolution and the operator who performed it MUST be recorded for audit.
- **FR-010**: System MUST detect head tasks that are in-flight but whose owning consumer has become unresponsive (lease/visibility timeout) and reconcile them through the standard retry-or-terminal-failure path before any later task for that entity is released.
- **FR-011**: System MUST tolerate at-least-once consumer behavior: a duplicate completion or duplicate failure report for the same head task MUST NOT cause out-of-order release of subsequent tasks.
- **FR-012**: System MUST expose, for each entity with pending work, observability sufficient to answer: how many tasks are queued, what the current head task is, and whether the entity is blocked (and if so, why — backoff vs. terminal failure).
- **FR-013**: System MUST preserve the ordering guarantee across restarts, deployments, and consumer scaling events; ordering state MUST be durable, not solely held in consumer memory.

#### Test Target Server Requirements

- **FR-014**: System MUST provide a target HTTP server that workers can be pointed at in place of the production HTTP destination, accepting the same request shape as the existing destination so that no worker code changes are needed to switch between the real destination and the test target.
- **FR-015**: The target server MUST track, per entity, the highest per-entity ordinal it has accepted, and on each incoming event compare the incoming ordinal against the expected next ordinal for that entity.
- **FR-016**: When the target server receives an event for an entity whose ordinal is lower than or equal-but-not-the-immediate-duplicate of the last accepted ordinal (i.e., a true out-of-order arrival), it MUST emit a structured log entry containing at minimum: entity identifier, expected next ordinal, received ordinal, and a timestamp; AND increment an ordering-violation counter metric, labeled at least by entity identifier (or a bucketed equivalent suitable for high-cardinality control).
- **FR-017**: When the target server receives a duplicate of an already-accepted event (same entity, same ordinal as the most recently accepted), it MUST treat the request as idempotently successful and MUST NOT count it as a violation.
- **FR-018**: The target server MUST support a configuration knob expressing what percentage of incoming requests to fail with HTTP 429 (rate-limited) and what percentage to fail with HTTP 5xx (server error), independently configurable, each in the range 0–100; the two configured rates combined MUST NOT exceed 100%.
- **FR-019**: Fault injection MUST be applied per-request using a deterministic-but-distributed selection (e.g., uniform random per request) such that, over a statistically sufficient sample, the observed failure rate matches the configured rate within reasonable tolerance.
- **FR-020**: The target server MUST expose its current fault-injection configuration, accepted-events-per-entity counts, and ordering-violation counts through an observability surface compatible with the rest of the project's existing observability approach.
- **FR-021**: The target server's fault-injection responses MUST use HTTP status semantics that the existing worker treats as transient/retryable, so that the backoff path defined in FR-006 is genuinely exercised.

### Key Entities *(include if feature involves data)*

- **Entity**: The domain object whose related events must be processed in order (e.g., a user, an account, a document). Identified by a producer-supplied identifier. Has zero or more pending coordinated tasks at any time.
- **Coordinated Task**: A unit of work belonging to exactly one entity. Carries a payload, a per-entity ordinal/position established at acceptance time, and a lifecycle state (pending, in-flight, awaiting-retry/backoff, succeeded, terminally-failed).
- **Entity Queue State**: The aggregate state of an entity's pending and in-flight tasks, including which task is the current head, whether the queue is blocked (backoff or terminal failure), and the next eligibility time if blocked.
- **Test Target Server**: A standalone HTTP service used as the worker's delivery destination during testing. Holds in-memory per-entity ordinal state to validate arrival order, holds the configured fault-injection rates, and exposes observability surfaces for accepted events, ordering violations, and current configuration. Not part of the production data path.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In automated tests submitting 1,000 tasks across 100 entities (10 each, in known order), 100% of tasks are observed by consumers in the exact submission order on a per-entity basis, across at least three consumer restarts during the run.
- **SC-002**: When the head task for an entity is forced into a backoff of duration *D*, zero subsequent tasks for that same entity are delivered to any consumer during the *D* interval, measured across 1,000 induced-failure trials.
- **SC-003**: While one entity's queue is blocked (backoff or terminal failure), end-to-end throughput for unblocked entities remains within 10% of baseline throughput measured with no blocked entities — i.e., one stuck entity does not degrade the rest of the system.
- **SC-004**: 100% of terminal-failure events on a head task produce an operator-visible signal (alert, dashboard indicator, or queryable status) within 1 minute of the event; zero terminal failures cause silent advancement of the entity's queue.
- **SC-005**: After an operator resolves a terminally-failed head task, the next task for that entity is eligible for delivery within one dispatch cycle (target: under 5 seconds in normal operating conditions).
- **SC-006**: Mean added end-to-end latency for in-order delivery (vs. an unordered baseline) is within an acceptable budget for the workload — target: under 50 ms p50 added latency for healthy entities under nominal load.
- **SC-007**: Running the test target server with fault injection disabled and a correctly-ordered input stream of at least 10,000 events across 100 entities produces zero ordering-violation log entries and a violation metric value of zero.
- **SC-008**: Running the test target server with a configured 429 rate of *X*% and a configured 5xx rate of *Y*% (X+Y ≤ 100), over at least 10,000 incoming requests, the observed 429 rate is within ±2 percentage points of *X*% and the observed 5xx rate is within ±2 percentage points of *Y*%.
- **SC-009**: When the test target server is integrated into an end-to-end test with non-zero fault-injection rates, the worker's backoff/retry path is exercised (verifiable by observing retry attempts) AND zero ordering violations are reported by the target server — confirming that the per-entity ordering guarantee holds under induced failures.

## Assumptions

- "Successful delivery" means the consumer has acknowledged the task as terminally successful per the existing task lifecycle; intermediate acknowledgements (e.g., "received") do not unblock the entity's queue.
- Order is established by the (single-instance) producer at message-generation time: the producer assigns a synthetic, strictly-increasing per-entity ordinal as each row is generated and writes it to the table in the same UPSERT. The producer's counter is in-memory only and is forgotten after the batch is written; subsequent batches for the same entity must therefore generate ordinals that are still greater than any previously-written ordinal for that entity (e.g., by basing the synthetic ordinal on a monotonic clock or a process-lifetime counter — see plan/research for the chosen approach).
- Cross-entity ordering is NOT a goal: tasks for different entities are independent and may interleave freely.
- The current backoff and retry policy of the existing coordinated-task system is reused; this feature constrains *when* later tasks become eligible, not *how* retries are scheduled.
- Operator intervention for terminally-failed head tasks is an existing or to-be-built operational workflow; this spec requires its existence and auditability but does not prescribe its UI.
- Entity identifiers are opaque strings supplied by the producer; the system does not interpret them beyond equality comparison for grouping.
- The test target server is a development/testing aid, not a production component. Its in-memory ordinal state is acceptable to lose on restart; persistence across runs is not required.
- The per-entity ordinal carried in each event (used by the test target to check ordering) is the same stable per-entity ordinal established at task acceptance time per FR-002, propagated to the destination as part of the event envelope.
- The test target's fault-injection configuration is set at startup (e.g., command-line flags or environment variables) and need not be runtime-mutable; if runtime adjustment becomes necessary, it can be added in a follow-up.
