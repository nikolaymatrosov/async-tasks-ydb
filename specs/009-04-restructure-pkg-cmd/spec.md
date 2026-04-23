# Feature Specification: Restructure 04 Example into pkg/cmd Layout

**Feature Branch**: `009-04-restructure-pkg-cmd`  
**Created**: 2026-04-23  
**Status**: Draft  
**Input**: User description: "I want add more structure to 04 example. Add pkg and cmd folders. Split producer and consumer into separate apps"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run Producer as Standalone Binary (Priority: P1)

A developer working with the 04 coordinated-table example wants to start only the producer process without starting a worker. They invoke a dedicated producer binary directly, passing only producer-relevant flags.

**Why this priority**: The split into separate binaries is the core goal; validating the producer standalone is the first proof-of-concept.

**Independent Test**: Build and run the producer binary with endpoint, database, and rate flags; confirm tasks are inserted into YDB without any worker process running.

**Acceptance Scenarios**:

1. **Given** the repository is built, **When** the developer runs `cmd/producer/producer --endpoint ... --database ...`, **Then** the binary starts, connects to YDB, and begins inserting tasks.
2. **Given** only worker-specific flags are absent, **When** the producer binary is invoked, **Then** no error is raised for missing worker flags.

---

### User Story 2 - Run Worker as Standalone Binary (Priority: P1)

A developer wants to start one or more worker processes independently, without co-locating them with the producer. They invoke a dedicated worker binary.

**Why this priority**: Equal priority to the producer — both binaries must work independently for the restructure to be complete.

**Independent Test**: Build and run the worker binary with endpoint, database, and worker-specific flags; confirm it polls partitions and processes tasks inserted by a separately-running producer.

**Acceptance Scenarios**:

1. **Given** the repository is built, **When** the developer runs `cmd/worker/worker --endpoint ... --database ...`, **Then** the binary starts, connects to YDB, and begins polling for tasks.
2. **Given** only producer-specific flags are absent, **When** the worker binary is invoked, **Then** no error is raised for missing producer flags.

---

### User Story 3 - Reuse Shared Logic via pkg (Priority: P2)

A developer wants to import shared types and helpers (e.g., YDB connection setup, task row definition, metrics) from a common package rather than duplicating code across the two binaries.

**Why this priority**: Without shared packages the binaries cannot be independently built without code duplication; this is the structural prerequisite.

**Independent Test**: Both binaries compile cleanly while importing shared logic from the `pkg/` subtree; no source file is duplicated between `cmd/producer` and `cmd/worker`.

**Acceptance Scenarios**:

1. **Given** shared logic lives under `pkg/`, **When** either binary is compiled, **Then** it resolves all dependencies from `pkg/` with no copy-paste duplication.
2. **Given** a shared type is updated in `pkg/`, **When** both binaries are rebuilt, **Then** both pick up the change without additional modifications.

---

### Edge Cases

- What happens if a developer tries to pass a producer-only flag (e.g., `--rate`) to the worker binary? The worker binary should reject unknown flags and exit with a clear error.
- What happens if a developer tries to pass a worker-only flag (e.g., `--lock-duration`) to the producer binary? Same: reject unknown flags with a clear error.
- If shared package code is changed in a breaking way, both binaries should fail to compile, making the breakage visible immediately.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The `04_coordinated_table` directory MUST be reorganised so that producer entry-point code lives under `cmd/producer/` and worker entry-point code lives under `cmd/worker/`.
- **FR-002**: Shared logic (types, YDB helpers, metrics, display, rebalancer, utils) MUST be extracted into one or more packages under `pkg/`.
- **FR-003**: Each of the two binaries MUST be independently buildable and runnable without the other being present or running.
- **FR-004**: The producer binary MUST accept all flags that were previously accepted by `--mode producer` (endpoint, database, partitions, coordination-path, rate, batch-window, report-interval, metrics-port).
- **FR-005**: The worker binary MUST accept all flags that were previously accepted by `--mode worker` (endpoint, database, partitions, coordination-path, lock-duration, backoff-min, backoff-max, metrics-port).
- **FR-006**: The `--mode` flag MUST be removed from both binaries (each binary's purpose is encoded in its name).
- **FR-007**: Existing functional behaviour (task insertion, partition lock, coordination, metrics) MUST be preserved unchanged.

### Key Entities

- **Producer binary** (`cmd/producer`): Entry point that connects to YDB and inserts tasks at a configured rate.
- **Worker binary** (`cmd/worker`): Entry point that connects to YDB, acquires partition locks, and processes tasks.
- **Shared packages** (`pkg/`): Reusable code consumed by both binaries — connection setup, task types, metrics, display, rebalancer, coordination utilities.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Both `cmd/producer` and `cmd/worker` binaries compile and run successfully with zero changes to their runtime behaviour.
- **SC-002**: No source file under `cmd/producer` or `cmd/worker` duplicates logic already present in `pkg/`; each logical unit exists in exactly one location.
- **SC-003**: A developer unfamiliar with the previous flat layout can identify the purpose of each binary and each shared package within 2 minutes of reading the directory tree.
- **SC-004**: The `--mode` flag is entirely absent from both binaries; passing it causes an explicit "unknown flag" error.
- **SC-005**: All existing capabilities (task production, worker coordination, Prometheus metrics) continue to work end-to-end after the restructure.

## Assumptions

- The Go module root and `go.mod` file remain at the repository root; no new module is introduced.
- Terraform and other infrastructure files outside `04_coordinated_table/` are not in scope.
- No new runtime behaviour is introduced; this is a pure structural refactor.
- The `testhelper` package at the repo root is out of scope.
- README updates for `04_coordinated_table/` to reflect the new layout are considered in scope.
