# Implementation Plan: Example 03 — Direct Topic Writer

**Branch**: `001-03-topic-writer` | **Date**: 2026-03-16 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-03-topic-writer/spec.md`

## Summary

Add a self-contained example (`03_topic/main.go`) that writes messages directly to a YDB topic with consistent key-to-partition routing. A `Producer` struct mirrors the reference in `docs/producer.go`: it enumerates active partitions at startup, creates one `topicwriter.Writer` per partition pinned by partition ID, and routes each `Write` call via murmur3 32-bit hash (`github.com/twmb/murmur3`) of the caller-supplied partition key. A thin `safeWriter` wrapper provides exponential-backoff retry on transport errors and indefinite retry on `ErrQueueLimitExceed`. The example publishes a fixed set of messages with at least two keys and prints per-message delivery confirmation, then cleanly shuts down all writers.

## Technical Context

**Language/Version**: Go 1.26 (as declared in go.mod)
**Primary Dependencies**: `github.com/ydb-platform/ydb-go-sdk/v3 v3.127.0`, `github.com/ydb-platform/ydb-go-yc v0.12.3`
**Storage**: YDB topic (write path only — no DB table interaction)
**Testing**: Manual end-to-end run against a live YDB instance (consistent with other examples in the project)
**Target Platform**: Linux/macOS server process (no cross-compilation constraints)
**Project Type**: Standalone runnable example (`go run ./03_topic/`)
**Performance Goals**: Demonstrate correct behaviour; throughput is secondary to correctness
**Constraints**: Uses existing `go.mod` dependencies — `cenkalti/backoff/v4` and `twmb/murmur3` were already present as indirect dependencies (now promoted to direct use); backoff constants: max interval 30 s, max elapsed time 5 min
**Scale/Scope**: Single binary; writes ~10 messages across 2 partition keys to prove routing behaviour

## Constitution Check

*GATE: Constitution template is unfilled (blank placeholders). No project-specific gates are defined.*

There are no active constitution rules to check. This plan proceeds on the conventions observed across existing examples:

| Convention observed | Compliance |
| ------------------- | ---------- |
| Single `main.go` per example directory | Compliant — `03_topic/main.go` |
| `YDB_ENDPOINT` + `YDB_SA_KEY_FILE` env vars | Compliant |
| `signal.NotifyContext` for graceful shutdown | Compliant |
| Final stats printed to stdout on exit | Compliant |
| No new `go.mod` dependencies | Compliant — uses only existing SDK packages |

*Re-check after Phase 1 design*: No violations introduced by the design artifacts below.

## Project Structure

### Documentation (this feature)

```text
specs/001-03-topic-writer/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

```text
03_topic/
├── main.go          # Producer + safeWriter implementation + main() demo loop
└── README.md        # Usage instructions (mirrors 01_db_producer/README.md style)
```

**Structure Decision**: Single flat example directory, consistent with `01_db_producer/` and `02_cdc_worker/`. All logic lives in `main.go` for maximum readability.

## Complexity Tracking

No constitution violations. Table omitted.
