# Feature Specification: Topic Partition Benchmark

**Feature Branch**: `002-topic-partition-bench`
**Created**: 2026-03-16
**Status**: Draft
**Input**: User description: "Topic partitioning benchmark measuring TLI errors across partition key strategies"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run Partition Benchmark End-to-End (Priority: P1)

As a developer, I want to run a single command that produces messages and consumes them under 4 different partition/table scenarios, so that I can see a side-by-side comparison of transaction lock contention caused by each partitioning strategy.

**Why this priority**: This is the core value of the feature. Without end-to-end orchestration, the benchmark cannot produce any actionable insight.

**Independent Test**: Can be tested by running the benchmark command with default settings and verifying that a comparison table with 4 rows of results is printed to the console.

**Acceptance Scenarios**:

1. **Given** topics and tables exist in the database, **When** the user runs the benchmark with default flags, **Then** the system publishes messages to both topics and runs all 4 consumer scenarios sequentially, printing a formatted comparison table at the end.
2. **Given** the benchmark is running, **When** all messages for a scenario have been consumed, **Then** the scenario completes and the next one begins automatically.
3. **Given** the benchmark completes, **When** the user reads the output table, **Then** each row shows the scenario name, message count, TLI error count, duration, and throughput (messages/second).

---

### User Story 2 - Observe Low Contention with User-Aligned Partitioning (Priority: P1)

As a developer, I want to see that partitioning messages by user ID results in significantly fewer transaction lock conflicts when updating per-user counters, so that I understand the benefit of aligning partition keys with data access patterns.

**Why this priority**: Demonstrating the "good" partitioning outcome is the primary educational goal of the benchmark.

**Independent Test**: Can be tested by comparing the TLI error count of the "by_user to stats" scenario against the "by_message_id to stats" scenario and verifying the former is substantially lower.

**Acceptance Scenarios**:

1. **Given** messages are partitioned by user ID, **When** multiple consumers process the counter table concurrently, **Then** TLI errors are low because each user's messages are handled by a single consumer.
2. **Given** messages are partitioned by message ID, **When** multiple consumers process the counter table concurrently, **Then** TLI errors are high because the same user's messages are spread across consumers, causing row-level lock conflicts.

---

### User Story 3 - Verify Insert-Only Workloads Are Contention-Free (Priority: P2)

As a developer, I want to see that insert-only operations (writing unique message IDs to the processed table) produce near-zero TLI errors regardless of partitioning strategy, so that I understand contention only arises from read-modify-write patterns on shared rows.

**Why this priority**: Provides the control case that isolates the variable being tested (partition key alignment with data access pattern).

**Independent Test**: Can be tested by checking that both "processed" table scenarios report zero or near-zero TLI errors.

**Acceptance Scenarios**:

1. **Given** messages are consumed from either topic, **When** processing only inserts unique IDs to the processed table, **Then** TLI errors are zero or near-zero regardless of partition key strategy.

---

### User Story 4 - Configure Benchmark Parameters (Priority: P3)

As a developer, I want to adjust the number of users, total messages, and topic paths via command-line flags, so that I can tailor the benchmark to my environment and observe how scale affects contention.

**Why this priority**: Configurability enables experimentation but is not required for the core demonstration.

**Independent Test**: Can be tested by running the benchmark with custom flag values and verifying the output reflects the provided parameters.

**Acceptance Scenarios**:

1. **Given** the user provides a custom message count, **When** the benchmark runs, **Then** all scenarios produce and consume exactly that many messages.
2. **Given** the user provides a custom user count, **When** messages are generated, **Then** user IDs are sampled from the specified pool size with a weighted distribution.

---

### Edge Cases

- What happens when a consumer falls behind and messages expire from the topic before being read?
- How does the system handle a scenario where the database becomes temporarily unavailable during consumption?
- What happens if the number of active topic partitions changes between the produce and consume phases?
- What happens if the user specifies zero messages or zero users?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST generate a configurable number of messages, each containing a unique identifier, a user identifier sampled from a weighted distribution, and a type category (one of three types).
- **FR-002**: System MUST publish each generated message to two separate topics: one partitioned by user identifier and one partitioned by message identifier.
- **FR-003**: System MUST consume messages using one dedicated reader per partition, with each reader running concurrently.
- **FR-004**: System MUST execute 4 scenarios sequentially: (1) user-partitioned topic with counter table, (2) user-partitioned topic with ID-only table, (3) message-partitioned topic with counter table, (4) message-partitioned topic with ID-only table.
- **FR-005**: System MUST track the count of transaction lock invalidation errors during each scenario using per-message transactions for the counter table workload.
- **FR-006**: System MUST reset the counter table between scenarios that modify it, so results are independent.
- **FR-007**: System MUST stop each scenario after consuming exactly the target number of messages.
- **FR-008**: System MUST display a formatted comparison table showing scenario name, messages processed, TLI error count, elapsed time, and throughput for all 4 scenarios.
- **FR-009**: System MUST accept command-line flags for user count (default 100), message count (default 100,000), and topic paths.
- **FR-010**: System MUST ensure data correctness: the sum of all per-user counters in the counter table MUST equal the total message count after each counter-table scenario completes.

### Key Entities

- **Message**: A benchmark payload with a unique ID, a user identifier, and a type category (A, B, or C). Generated in bulk before publishing.
- **Counter Record**: A per-user row tracking counts of each message type. Updated via read-modify-write transactions (the contention-prone workload).
- **Processed Record**: A per-message row recording that a message was consumed. Written via simple inserts (the contention-free workload).
- **Scenario Result**: Metrics collected for each of the 4 benchmark runs: message count, TLI errors, duration, and throughput.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The "user-partitioned topic with counter table" scenario produces at least 10x fewer TLI errors than the "message-partitioned topic with counter table" scenario when run with default settings.
- **SC-002**: Both "processed table" scenarios produce zero or near-zero TLI errors regardless of partitioning strategy.
- **SC-003**: All 100,000 messages (default setting) are consumed in every scenario, with no messages lost or double-counted.
- **SC-004**: The counter table is verified correct after each counter-table scenario (sum of all counters equals total messages).
- **SC-005**: The benchmark completes all 4 scenarios and prints a human-readable comparison table without manual intervention.
