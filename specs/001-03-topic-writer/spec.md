# Feature Specification: Example 03 — Direct Topic Writer

**Feature Branch**: `001-03-topic-writer`
**Created**: 2026-03-16
**Status**: Draft
**Input**: User description: "I want one more example 03_topic where we will write straight to the topic. Look at docs/producer.go as a reference of implementation of consistent writing messages with a given key to a single partition."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Write Messages to Topic with Consistent Partition Routing (Priority: P1)

A developer running the example application publishes a batch of messages to a YDB topic, each carrying a partition key. The system ensures all messages sharing the same key are always delivered to the same topic partition, guaranteeing ordering for any given key.

**Why this priority**: Consistent key-to-partition routing is the core value of this example — without it the example adds nothing beyond trivial topic writes.

**Independent Test**: Run the example binary with a set of messages sharing the same key; verify all messages land on the same partition by reading the topic offset/partition metadata.

**Acceptance Scenarios**:

1. **Given** a running YDB instance with an existing topic, **When** the example starts and publishes N messages with the same partition key, **Then** all N messages are written to the same partition.
2. **Given** two different partition keys, **When** messages with each key are published, **Then** messages consistently map to their respective partitions across multiple runs (deterministic hashing).
3. **Given** the example starts successfully, **When** a message is written, **Then** the write is acknowledged by the server before the function returns (wait-for-ack semantics).

---

### User Story 2 - Resilient Writes with Automatic Retry (Priority: P2)

When a transient error occurs during a write attempt (e.g., a brief network interruption), the example automatically retries the write using exponential backoff instead of immediately failing, so the developer can observe that reliable delivery is built in.

**Why this priority**: Resilient writes are a key production concern; demonstrating this in the example teaches the correct pattern.

**Independent Test**: Introduce a simulated transient error (or observe behaviour during real network interruptions); confirm the example retries and eventually succeeds without manual intervention.

**Acceptance Scenarios**:

1. **Given** a transient write failure, **When** the error is a retriable transport error, **Then** the example retries the write with exponential backoff up to a configured maximum elapsed time.
2. **Given** a permanent write error (non-transport), **When** the write fails, **Then** the example stops retrying and reports the error to the caller.
3. **Given** the write queue on the server is temporarily full (queue-limit-exceeded), **When** this condition occurs, **Then** the example keeps retrying indefinitely until the queue has space or the context is cancelled.

---

### User Story 3 - Graceful Startup and Shutdown (Priority: P3)

The example initialises all per-partition writers at startup and closes them cleanly on shutdown, so the developer can see the full lifecycle of a topic producer.

**Why this priority**: Proper resource management is important for production code; the example should demonstrate it even if it is not the primary focus.

**Independent Test**: Run the example to completion; confirm no leaked goroutines or open connections are reported.

**Acceptance Scenarios**:

1. **Given** a topic with multiple active partitions, **When** the example starts, **Then** a dedicated writer is created for each active partition.
2. **Given** the example is finished or receives a shutdown signal, **When** `Stop` is called, **Then** all partition writers are closed and any in-flight writes are flushed before the process exits.
3. **Given** `Start` is called a second time on the same producer instance, **When** it is already running, **Then** the example panics with a clear message (guard against double-init).

---

### Edge Cases

- What happens when the topic has no active partitions at startup?
- How does the system handle a context cancellation mid-write (write in progress)?
- What if the topic has only one partition (all keys route to the same partition)?
- How does the example behave if the YDB endpoint is unreachable at startup?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The example MUST connect to a YDB topic and enumerate its active partitions at startup.
- **FR-002**: The example MUST create one dedicated writer per active partition.
- **FR-003**: The example MUST route each message to a partition determined by a deterministic hash of its partition key (murmur3 32-bit algorithm via `github.com/twmb/murmur3`).
- **FR-004**: The example MUST wait for server acknowledgement before considering a write successful.
- **FR-005**: The example MUST retry write attempts on transient (transport-level) errors using exponential backoff with a maximum interval of 30 seconds and maximum elapsed time of 5 minutes.
- **FR-006**: The example MUST keep retrying indefinitely (within context) when the server-side write queue is full, without applying the maximum-elapsed-time limit.
- **FR-007**: The example MUST stop retrying and propagate the error on permanent (non-transport) failures.
- **FR-008**: The example MUST close all partition writers cleanly when stopped, collecting and joining any close errors.
- **FR-009**: The example MUST demonstrate writing a representative set of messages with at least two different partition keys to show routing behaviour.
- **FR-011**: Each message payload MUST be a JSON-serialised struct with randomly generated field values (e.g. UUID, random byte slice, timestamp).
- **FR-012**: The example MUST use `log/slog` (structured logging) for all log output.
- **FR-010**: The example MUST be self-contained in a dedicated `03_topic/` directory at the repo root (consistent with `01_db_producer/` and `02_cdc_worker/`) and runnable with a single command.

### Key Entities

- **Producer**: Manages a set of per-partition writers for a single topic; owns startup, routing, and shutdown logic.
- **Partition Writer**: Wraps a single-partition topic writer; handles retry logic and error classification.
- **Message**: A unit of data published to the topic; payload is a JSON-serialised struct with randomly generated field values (e.g. UUID, random bytes, timestamp).
- **Partition Key**: A string value used to deterministically select the target partition for a message.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All messages with the same partition key are delivered to the same topic partition in 100% of runs.
- **SC-002**: The example completes a batch of 10 messages without error on a healthy YDB instance.
- **SC-003**: After a simulated transient error, the example retries and successfully delivers all messages within the configured backoff window (5 minutes maximum elapsed time).
- **SC-004**: Shutdown completes within 5 seconds of receiving a stop signal with no open writers remaining.
- **SC-005**: The example is understandable without prior SDK knowledge — the primary write flow is visible end-to-end in the `main` function.

## Clarifications

### Session 2026-03-16

- Q: Which hash algorithm for partition key routing — FNV-1a or murmur3? → A: murmur3 (via `github.com/twmb/murmur3`); better distribution for short string keys.
- Q: Directory layout — `examples/03_topic/` or repo-root `03_topic/`? → A: `03_topic/` at repo root, consistent with existing examples.
- Q: Which topic to write to — new dedicated topic or existing `tasks/cdc_tasks`? → A: New dedicated topic `tasks/direct`; keeps example self-contained and avoids polluting the CDC stream.
- Q: What should demo message payloads contain? → A: A struct serialised to JSON with randomly generated field values.
- Q: Logging style — stdlib `log` or `log/slog`? → A: `log/slog` (structured, stdlib since Go 1.21, no new dependency).

## Assumptions

- The target topic is `tasks/direct`; it must be created once via `ydb topic create` before the example runs (documented in README).
- YDB connection credentials and endpoint are provided via environment variables, consistent with other examples in the project.
- Backoff defaults mirror the reference implementation: max interval 30 s, max elapsed time 5 minutes.
- The example does not handle topic creation at runtime.
- The example targets Go, consistent with the rest of the project codebase.
