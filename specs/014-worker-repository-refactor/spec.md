# Feature Specification: Worker Repository Refactor

**Feature Branch**: `014-worker-repository-refactor`
**Created**: 2026-04-25
**Status**: Draft
**Input**: User description: "refactor worker code. Extract database interactions into repository with well defined methods"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Isolate task-locking and completion data access behind a repository (Priority: P1)

As a developer maintaining the coordinated-table worker, I want all task-storage interactions (reading the next eligible task, atomically claiming it, marking it completed) to live behind a single repository abstraction with explicit, named methods, so that the worker's orchestration logic (partition lifecycle, lease management, backoff, processor invocation) can be read, reviewed, and modified without wading through embedded query strings, parameter builders, and transaction settings.

**Why this priority**: This is the entire point of the refactor. The worker file currently mixes high-level partition orchestration with low-level YDB query construction, three transaction blocks, and inline DDL strings. Without this story the codebase remains hard to review, hard to test, and hard to evolve. Every other priority depends on the boundary established here.

**Independent Test**: Can be fully validated by running the existing worker against a real YDB instance (or the existing integration setup) and confirming that tasks continue to be locked, processed, and completed at the same rate, with the same correctness guarantees, while a code reader can verify that no SQL strings, parameter builders, or transaction-control calls remain in the worker package outside the repository boundary.

**Acceptance Scenarios**:

1. **Given** the refactor is merged, **When** a developer reads the worker's partition-processing function, **Then** they see method calls expressing intent (e.g., "fetch the next eligible task in this partition", "atomically claim this task with a lock", "mark this task completed") rather than embedded query text, transaction settings, or result-set scanning.
2. **Given** the same task table and the same set of producer-inserted tasks, **When** workers run before vs. after the refactor under identical load, **Then** the observable outcomes — tasks-per-second processed, lock-contention behavior, expired-lock reclaim behavior, and stale-task handover between owners — are unchanged within normal run-to-run variance.
3. **Given** a unit test for the repository, **When** the test exercises each repository method against a controlled task-table state, **Then** each method's behavior (rows returned, rows updated, conditional no-op when status changes mid-flight) can be verified in isolation from the worker's goroutine and lease machinery.

---

### User Story 2 - Make the worker testable without a live database (Priority: P2)

As a developer adding new worker behavior (for example, retry rules, dead-lettering, or priority changes), I want to substitute a fake or in-memory implementation of the task-storage boundary in tests, so that I can verify orchestration logic — backoff escalation, partition cancellation, lease loss, concurrent completion — without standing up a YDB instance and without flaky timing dependencies on real query latency.

**Why this priority**: The current worker can only be tested end-to-end against a real database; pure orchestration bugs (e.g., backoff math, lease handover, goroutine cleanup) cannot be exercised in isolation. Once the repository boundary exists, this becomes possible — but the refactor itself must define the boundary in a way that supports substitution. This is a direct consequence of P1, but it is called out separately because it constrains the shape of the abstraction (it must be substitutable, not just extracted).

**Independent Test**: A test can construct a worker with an in-memory or scripted task-storage stand-in that returns predetermined sequences (no eligible task, an eligible task, a transient error, a lost-race no-op) and assert that the worker's backoff, locking, and completion behavior responds correctly — all without any database connection.

**Acceptance Scenarios**:

1. **Given** a worker constructed with a scripted task-storage stand-in that always returns "no eligible task", **When** the worker runs for several iterations, **Then** the backoff interval grows from the configured minimum toward the configured maximum and the worker never invokes the task processor.
2. **Given** a stand-in that returns an eligible task on the first call but reports "lost race" on the claim step, **When** the worker processes one iteration, **Then** the worker treats the iteration as a no-op (no processor invocation, no completion call) and continues looping.
3. **Given** a stand-in that returns a successfully-claimed task, **When** the worker invokes the processor and the processor succeeds, **Then** the worker calls the repository's "mark completed" method exactly once with the task's identity and lock information.

---

### User Story 3 - Centralize the task-table contract for future producers and consumers (Priority: P3)

As a developer extending the system (e.g., the existing producer in the same package, a future archiver, a future admin tool), I want the column names, status values, and conditional-claim semantics of the `coordinated_tasks` table to be expressed in one place, so that schema or status-vocabulary changes (e.g., adding a "failed" status, renaming `locked_until`, adjusting the partition-key shape) require edits in a single, named module instead of grep-and-replace across query strings.

**Why this priority**: The producer already has its own embedded `UPSERT` against the same table; today both worker and producer hardcode column names and the `pending`/`locked`/`completed` vocabulary independently. Consolidating the worker's side is the prerequisite to eventually doing the same for the producer. P3 because the immediate refactor scope is the worker; this story documents the longer-term direction the abstraction should support, not work to do now.

**Independent Test**: A reader can locate every reference to `coordinated_tasks` column names, status string literals, and partition-key shape used by the worker by inspecting a single repository module, with no occurrences elsewhere in the worker package.

**Acceptance Scenarios**:

1. **Given** a hypothetical future renaming of the `locked_until` column, **When** a developer searches the worker package for the old name, **Then** all matches are confined to the repository module.
2. **Given** the repository module, **When** a reader scans its public surface, **Then** the named methods collectively express the full set of task-table operations the worker needs (read candidate, conditional claim, mark complete) with no operation requiring callers to compose their own queries.

---

### Edge Cases

- A snapshot read returns a candidate task, but by the time the worker attempts the conditional claim, another owner (after lease handover) has already locked or completed it — the repository's claim method must report this as a clean no-op rather than an error, and the worker must continue without invoking the processor or the completion call.
- A claim succeeds but the worker's own processor function returns an error — the repository must NOT mark the task completed; the lock will expire naturally via `locked_until` and become reclaimable. (This preserves current behavior; no new "mark failed" path is introduced by this refactor.)
- The partition lease is lost mid-iteration (lease context cancelled) — repository calls already in flight should propagate cancellation; subsequent iterations must not be started. The repository must not silently retry across a cancelled context.
- Two workers race for the same task because of overlapping lease handover — exactly one claim must succeed; the loser's repository call must return the same "no-op" result that distinguishes "no eligible task" from "candidate was taken".
- The repository is invoked with a partition identifier outside the configured range — the repository must not panic; it must return the same "no eligible task" result a real query would (no rows match).
- A task's `scheduled_at` is in the future when the candidate is selected — the repository must not return it; this scheduling filter is part of the repository's contract, not the worker's.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The worker package MUST expose a single, named storage abstraction (referred to here as the "task repository") that owns every interaction with the `coordinated_tasks` table currently performed by the worker.
- **FR-002**: The task repository MUST provide a method to fetch the highest-priority eligible task for a given partition, where "eligible" means status is `pending`, OR status is `locked` with an expired `locked_until`, AND `scheduled_at` is null or already past — returning either a single task descriptor or a clean "none available" signal.
- **FR-003**: The task repository MUST provide a method to atomically claim a previously-fetched candidate by setting it to `locked` with a caller-supplied lock value and lock-until deadline, conditional on the row still being claimable; the method MUST distinguish the "claim succeeded" outcome from the "lost the race / no longer claimable" outcome without surfacing this distinction as an error.
- **FR-004**: The task repository MUST provide a method to mark a previously-claimed task as `completed`, recording a completion timestamp.
- **FR-005**: The Worker type MUST NOT contain any embedded query strings, parameter-builder calls, transaction-setting calls, or result-set scanning code after the refactor; all such code MUST live inside the task repository module.
- **FR-006**: The Worker type MUST be constructible with the task repository as a substitutable dependency, so that tests can supply an alternative implementation without a live database.
- **FR-007**: Observable behavior under load — task throughput, lock-claim correctness, expired-lock reclamation, lease-handover safety, completion semantics — MUST be unchanged relative to the pre-refactor worker. The refactor MUST NOT introduce new statuses, new columns, new query patterns, or new transaction boundaries; the same two-phase "snapshot select → conditional point update" sequence and the same serializable transaction for completion MUST be preserved.
- **FR-008**: Logging emitted by the worker MUST remain at the worker layer (not move into the repository), preserving the existing structured-log fields (`worker_id`, `partition_id`, `task_id`, `priority`, `err`) and the existing error/warn/info levels at each call site.
- **FR-009**: Metric updates (locked count, processed count, error count, partitions-owned gauge) MUST remain at the worker layer; the repository MUST NOT directly increment metrics counters.
- **FR-010**: The repository's method signatures MUST be expressed in domain terms (partition identifier, task identifier, lock value, lock-until deadline, completion time) rather than database-vendor terms; callers MUST NOT need to know about parameter builders, transaction control objects, or session types to use them.
- **FR-011**: Errors returned by the repository MUST allow the worker to distinguish (a) "context cancelled / lease lost" — which the worker treats as a normal exit, from (b) "transient backend error" — which the worker treats as a backoff trigger, from (c) "no eligible task / lost race" — which is not an error at all.
- **FR-012**: The `coordinated_tasks` schema, including all column names, types, status vocabulary (`pending`, `locked`, `completed`), and partition-key shape, MUST remain unchanged. This refactor is code-only.

### Key Entities

- **Task Repository**: The named abstraction owning all `coordinated_tasks` reads and writes performed by the worker. Its public surface consists of three operations expressed in domain terms: fetch-eligible-candidate, conditional-claim, and mark-completed.
- **Task Descriptor**: The minimal value the repository returns to the worker after fetching a candidate or after a successful claim — carrying task identity, partition, priority, payload, and (post-claim) the lock value and lock-until deadline. The worker hands a descriptor back to the repository to identify which row to claim or complete; it does not synthesize partition/priority/id tuples on its own.
- **Eligibility Predicate**: The conjunction of conditions that defines whether a task may be claimed in a given partition (status `pending` or expired `locked`, AND `scheduled_at` ≤ now or null). The repository — not the worker — owns this definition.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After the refactor, the file containing the Worker type contains zero query strings, zero references to parameter-builder constructors, and zero references to transaction-control settings — verifiable by static inspection (text search) of the worker source file.
- **SC-002**: The worker's partition-processing function reads as a sequence of named operations (fetch / claim / process / complete) and is at least 30% shorter (in non-blank, non-comment lines) than the pre-refactor `lockNextTask` + `completeTask` pair, while preserving identical control flow at the orchestration level.
- **SC-003**: Throughput and error counters under a fixed producer rate, sampled over a 10-minute steady-state run, are within ±5% of pre-refactor values for tasks-locked-per-second, tasks-completed-per-second, and lock-claim-error rate.
- **SC-004**: A new test suite covers the worker's orchestration logic (backoff escalation, lost-race no-op handling, lease cancellation, processor-error path) without requiring a database — verifiable by the suite passing in an environment with no YDB connection configured.
- **SC-005**: Modifying the eligibility predicate (e.g., to add a `failed` status) requires an edit in exactly one file inside the worker package; the worker file itself requires no change.
- **SC-006**: Code review time for a follow-up worker change (any change touching task fetching or completion) is reduced because the reviewer can read the worker without simultaneously parsing embedded SQL — qualitatively measured by the reviewer being able to summarize the change without referencing query syntax.

## Assumptions

- The producer's existing `UPSERT` against `coordinated_tasks` is **out of scope** for this refactor. The user request says "refactor worker code"; consolidating the producer side is a future story (User Story 3 documents the direction without requiring producer changes here).
- The two-phase locking strategy (snapshot read followed by serializable conditional update) is intentional — it exists to avoid range read-locks that would invalidate concurrent producer `UPSERT`s. The refactor preserves this strategy verbatim; it does not "simplify" it into a single transaction.
- The existing `ProcessTask` callback (the user-supplied task-processing function) is unchanged in shape; the refactor touches only the storage boundary, not the processing-function interface.
- The repository implementation will continue to use the existing YDB driver and query patterns; no migration to a different database client or to `database/sql` is implied.
- The package layout follows the existing project convention (the repository will live alongside the worker in the `pkg/taskworker` directory or a closely related sibling package, matching the existing structure under `04_coordinated_table/pkg/`).
- Logging continues to use the existing `log/slog` structured logger at the worker layer; the repository does not log.
- Metrics continue to use the existing counters and gauges in the metrics package; the repository does not touch them.
- "Well-defined methods" in the user request is interpreted as: each method has a clear domain-level name, a documented contract for its three outcomes (success / no-op / error), and parameters expressed in domain terms — not as a mandate to introduce a generic CRUD interface.
