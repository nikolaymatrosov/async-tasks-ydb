# Implementation Plan: Per-Entity Ordered Task Delivery

**Branch**: `017-entity-task-ordering` | **Date**: 2026-04-29 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/017-entity-task-ordering/spec.md`

## Summary

Add a per-entity FIFO ordering guarantee on top of the coordinated-task pattern, **forked into a
new self-contained example `05_ordered_tasks/`** (the existing `04_coordinated_table/` is left
untouched). Each task is tagged with `entity_id` and a producer-assigned synthetic monotonic
ordinal `entity_seq`; workers may dispatch only the *head* task for each entity (lowest
non-completed `entity_seq`); subsequent tasks for an entity stay invisible during in-flight
processing, retry backoff, and terminal failure. The schema drops `priority` (no longer needed)
and routes `partition_id` by `murmur3(entity_id) % partitions`.

The producer is a **single-instance process** that generates tasks directly into the new
`ordered_tasks` table. There is **no topic, no relay**: when the producer fabricates a new
`entity_id` it also picks a synthetic starting offset, then increments that offset for each
subsequent message it generates for that entity within the same batch, writes the rows in one
batch UPSERT, and forgets the counter immediately. To guarantee monotonicity *across* batches for
a given entity_id (in the rare case the producer regenerates the same id), the synthetic offset is
derived from a process-wide strictly-increasing source (`time.Now().UnixNano()` plus an
in-process atomic tiebreaker) rather than a per-entity counter — see Research §2.

To exercise the guarantee end-to-end, a new self-contained `06_target_server/` example provides an
HTTP destination that records the highest accepted `entity_seq` per entity, flags rewind arrivals
as structured-log + metric violations, treats same-ordinal redeliveries as idempotent successes,
and supports configurable HTTP 429 / 5xx fault-injection rates so the worker's backoff path is
genuinely exercised.

## Technical Context

**Language/Version**: Go 1.26 (as declared in `go.mod`)
**Primary Dependencies**: `github.com/ydb-platform/ydb-go-sdk/v3 v3.135.0` (`query`, `query.Session`,
`query.TxActor`, `ParamsBuilder`, `types.AS_TABLE` list-struct path), `github.com/twmb/murmur3 v1.1.8`
(partition routing), `github.com/google/uuid v1.6.0` (task ids), stdlib `net/http`, `log/slog`,
`context`, `time`, `flag`, `sync/atomic`. **No new direct `go.mod` dependencies.**
**Storage**: A new YDB table `ordered_tasks` (separate from `coordinated_tasks`) created by a
goose migration. Schema mirrors `coordinated_tasks` minus `priority` and `hash`, plus the
ordering columns (`entity_id`, `entity_seq`, `attempt_count`, `last_error`, `resolved_by`,
`resolved_at`) and the extended status vocabulary (`pending`, `locked`, `completed`, `failed`,
`skipped`). The target server is in-memory only.
**Testing**: Manual end-to-end validation per constitution (no automated suite). Acceptance
baseline = slog output of the worker + `/state` and `/metrics` from the target server against a
seeded YDB instance (see [quickstart.md](./quickstart.md)).
**Target Platform**: Linux server (workers + target server can run as containers; both also
runnable locally via `go run`).
**Project Type**: Two new self-contained examples (`05_ordered_tasks/`, `06_target_server/`).
`04_coordinated_table/` is unmodified.
**Performance Goals**: Per-entity dispatch overhead ≤ 50 ms p50 added latency on healthy entities
under nominal load (SC-006); end-to-end throughput on unblocked entities stays within 10 % of the
no-blocked-entity baseline (SC-003); target server sustains the producer's batch rate.
**Constraints**: Per-entity ordering must hold across worker restarts and partition rebalances
(FR-013) — therefore ordering state lives in YDB, not in worker memory. Backoff is expressed as a
`scheduled_at` on the head task itself so that the eligibility predicate naturally hides the
entity for the full backoff window (FR-006). Fault-injection percentages on the target server
must combine to ≤ 100 (FR-018). Producer is single-instance (per Clarifications); concurrent
producers for the same entity are out of scope for this fork.
**Scale/Scope**: 256 logical partitions (matching `04_coordinated_table`'s convention). Up to
~10⁵ entities active concurrently; per-entity backlog up to 30 000 tasks (spec edge case).
Target-server in-memory ordinal map sized for the test workload.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Verify compliance with each principle in `.specify/memory/constitution.md v1.0.0`:

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | ⚠️ `06_target_server/main.go` is single-file ✅. `05_ordered_tasks/` mirrors `04_coordinated_table/`'s pre-existing `cmd/`+`pkg/` structure (producer + worker + shared packages) — same pattern as `04`, no new deviation. Both are runnable with `go run ./<example>/cmd/<binary>` (or `go run ./06_target_server`). |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ Producer, worker, and target server all use `signal.NotifyContext(SIGTERM, SIGINT)` and shutdown with `context.Background()` for deferred Stop/Close. |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ One new migration `20260429000007_create_ordered_tasks.sql` containing the full table + secondary index. Symmetric `+goose Down` drops the index and table. No runtime DDL. |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ Producer/worker reuse the existing env-var convention from `04_coordinated_table`; target server uses `LISTEN_ADDR`, `FAULT_429_PERCENT`, `FAULT_5XX_PERCENT`; no creds, no hardcoded URLs. |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ All three binaries use `slog.NewJSONHandler` with structured fields (`entity_id`, `entity_seq`, `expected_seq`, `received_seq`, `http_status`, `injected_fault`); operator-facing stats blocks printed to stdout on shutdown. |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ Producer routes `partition_id = murmur3.Sum32([]byte(entity_id)) % partitions`. Migration via goose. No new direct `go.mod` dependencies. |

No ❌; Complexity Tracking table is not required.

## Project Structure

### Documentation (this feature)

```text
specs/017-entity-task-ordering/
├── plan.md              # This file
├── spec.md              # Feature spec (with /speckit.clarify session 2026-04-29 applied)
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (target-server HTTP contract)
│   ├── target-server-ingest.md
│   └── target-server-observability.md
└── tasks.md             # Phase 2 output (created by /speckit.tasks)
```

### Source Code (repository root)

```text
05_ordered_tasks/                       # NEW — forked from 04_coordinated_table; priority dropped;
│                                       #   producer writes directly to table; no topic/relay.
├── cmd/
│   ├── producer/main.go                #   single-instance producer; synthetic monotonic seq
│   └── worker/main.go                  #   head-of-entity dispatcher; sets X-Entity-{ID,Seq} headers
├── pkg/
│   ├── taskproducer/producer.go        #   batch generator + AS_TABLE upsert (no MAX query, no tx)
│   ├── taskworker/
│   │   ├── repository.go               #   Candidate / ClaimedTask + 5 repo methods
│   │   ├── repository_ydb.go           #   FetchEligibleHeads, ClaimTask, MarkCompleted,
│   │   │                               #     MarkFailedWithBackoff, MarkTerminallyFailed
│   │   └── worker.go                   #   per-partition loop with attempt-count + max-attempts
│   ├── rebalancer/                     #   copied from 04_coordinated_table (256 partition leases)
│   ├── ydbconn/conn.go                 #   copied from 04_coordinated_table
│   ├── uid/uid.go                      #   copied from 04_coordinated_table
│   └── metrics/                        #   copied from 04_coordinated_table; entity-aware fields
└── README.md

06_target_server/                       # NEW — self-contained, single main.go (constitution I)
└── main.go                             # HTTP ingest + ordinal check (recv > last) + fault injection
                                        #   + /metrics + /state + /healthz + slog + signal shutdown

migrations/
└── 20260429000007_create_ordered_tasks.sql   # NEW (goose Up/Down) — full table + secondary index

cmd/migrate/main.go                                       # unchanged (uses goose)
04_coordinated_table/                                     # UNMODIFIED
```

**Structure Decision**: Fork `04_coordinated_table` into a new self-contained example
`05_ordered_tasks/` per the user's clarification. The fork (a) drops the `priority` column entirely
to keep the demonstration focused on per-entity ordering, (b) routes partitions by `entity_id`,
(c) keeps the producer as the sole writer of the new `ordered_tasks` table — no topic, no relay,
no read-modify-write transaction. The test target server lives in `06_target_server/` as a
single-`main.go` example per constitution principle I and is runnable independently with
`go run ./06_target_server`.

## Complexity Tracking

> No constitution violations to justify; this section is intentionally empty.
