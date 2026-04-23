# Research: 009-04-restructure-pkg-cmd

## Decision 1: Module layout — no new go.mod

**Decision**: Keep the single `go.mod` at the repo root (`module async-tasks-ydb`). Sub-packages under `04_coordinated_table/pkg/` and entry points under `04_coordinated_table/cmd/` are imported as `async-tasks-ydb/04_coordinated_table/pkg/<name>`.

**Rationale**: The repo already uses a single-module layout (all examples share `go.mod`). Introducing a nested module would require `go work` or `replace` directives and contradicts the assumption in spec §Assumptions ("The Go module root and go.mod file remain at the repository root; no new module is introduced").

**Alternatives considered**: Nested `go.mod` inside `04_coordinated_table/` — rejected; adds workspace complexity with no benefit for a refactor-only change.

---

## Decision 2: Package decomposition

**Decision**: Split into six shared packages plus two entry points:

| Package | Import path | Contents |
|---|---|---|
| `pkg/uid` | `async-tasks-ydb/04_coordinated_table/pkg/uid` | `generateUUID` |
| `pkg/metrics` | `async-tasks-ydb/04_coordinated_table/pkg/metrics` | `metricsHandler`, `Stats`, `ProducerStats`, `readCounter`, `readGauge` |
| `pkg/rebalancer` | `async-tasks-ydb/04_coordinated_table/pkg/rebalancer` | `Rebalancer`, `partitionEvent`, `ceilDiv` |
| `pkg/taskworker` | `async-tasks-ydb/04_coordinated_table/pkg/taskworker` | `Worker`, `lockedTask`, `minDuration` |
| `pkg/taskproducer` | `async-tasks-ydb/04_coordinated_table/pkg/taskproducer` | `taskRow`, `buildBatch`, `upsertBatch`, `produce` |
| `pkg/ydbconn` | `async-tasks-ydb/04_coordinated_table/pkg/ydbconn` | credential resolution + `Open` helper |

**Rationale**: Each package groups types and functions by their cohesion domain, with no circular dependency. `taskworker` avoids the name collision with `cmd/worker` (Go allows it, but distinct names improve readability). `ydbconn` eliminates the duplicated credential-resolution block that would otherwise appear in both `cmd/producer/main.go` and `cmd/worker/main.go`.

**Alternatives considered**: Single `pkg/shared` mega-package — rejected; defeats the purpose of structured layout and makes import graphs opaque.

---

## Decision 3: Coordination node creation placement

**Decision**: Coordination node creation (`db.Coordination().CreateNode(...)`) moves to `cmd/worker/main.go` only. The producer does not need a coordination node.

**Rationale**: In the current `main.go`, CreateNode runs unconditionally before the mode switch — but the producer never uses coordination. The split reveals this correctly: the producer's binary has no coordination imports; the worker's binary performs CreateNode before starting the rebalancer.

**Alternatives considered**: Keep CreateNode in a shared helper — rejected; the producer has no reason to run this; it would add a Coordination() call and an import the producer doesn't need.

---

## Decision 4: `newUUID` wrapper placement

**Decision**: `newUUID` (the panic-on-error wrapper) moves to `cmd/worker/main.go` since it is only called there (to generate `workerID`). `generateUUID` (the error-returning primitive) stays in `pkg/uid` for use by both.

**Rationale**: The producer calls `generateUUID` directly inside `buildBatch` via `taskproducer` package; the worker calls `newUUID` at startup. The wrapper is entry-point-specific glue code, not a shared utility.

---

## Decision 5: Constitution Principle I deviation

**Decision**: Deviation from Principle I ("All logic for an example MUST reside in a single `main.go` file — no sub-packages within an example directory") is justified and documented.

**Rationale**: The feature's explicit goal is to demonstrate a multi-binary, pkg/cmd-structured Go project — a pattern that by definition cannot fit in a single `main.go`. The constitution's intent (readability, minimal overhead) is preserved by keeping each binary's `main.go` as the sole entry point and isolating reusable logic in well-named packages.

**Simpler alternative rejected**: Keeping a single binary with `--mode` is the status quo; the spec explicitly removes `--mode` (FR-006).
