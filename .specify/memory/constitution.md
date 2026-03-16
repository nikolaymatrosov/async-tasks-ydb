<!--
  SYNC IMPACT REPORT
  ==================
  Version change: [unfilled template] → 1.0.0
  Modified principles: N/A (initial ratification)
  Added sections:
    - Core Principles (I–V)
    - Technology Constraints
    - Development Workflow
    - Governance
  Removed sections: N/A
  Templates reviewed:
    ✅ .specify/templates/plan-template.md — Constitution Check section present; no changes needed
    ✅ .specify/templates/spec-template.md — aligned with principle I (self-contained) and V (slog)
    ✅ .specify/templates/tasks-template.md — aligned with principle II (lifecycle) and III (migrations)
    ✅ .specify/templates/agent-file-template.md — generic; no agent-specific overrides needed
  Deferred TODOs: none
-->

# async-tasks-ydb Constitution

## Core Principles

### I. Self-Contained Examples

Every runnable example MUST live in its own top-level directory (e.g., `01_db_producer/`,
`02_cdc_worker/`, `03_topic/`). All logic for an example MUST reside in a single `main.go`
file — no sub-packages within an example directory. Each example MUST be runnable with a
single command (`go run ./<example>/`) without modifying any other part of the repo.

**Rationale**: Flat, single-file layout maximises readability for developers learning the SDK.
Sub-packages introduce import paths and navigation overhead that obscure the core pattern.

### II. Lifecycle Completeness

Every example MUST demonstrate a full producer or consumer lifecycle:

- **Startup**: enumerate resources (partitions, tables) and allocate writers/readers.
- **Operation**: perform the primary action (write, read, process).
- **Shutdown**: close all resources, collect errors, and exit cleanly on `SIGTERM`/`SIGINT`
  via `signal.NotifyContext`. Deferred `Stop`/`Close` calls MUST use `context.Background()`
  so shutdown is not cancelled by the main context.

**Rationale**: Examples that leak goroutines or connections teach incorrect production patterns.

### III. Schema-Managed Persistence

All YDB schema changes (tables, changefeeds, topics) MUST be expressed as goose migrations
in the `migrations/` directory. Migration files MUST follow the naming convention
`YYYYMMDDNNNNNN_<description>.sql` and include both `+goose Up` and `+goose Down` sections.
No example MUST create schema at runtime.

**Rationale**: Versioned migrations ensure reproducibility across environments and team members.
Runtime DDL in example code conflates setup with the demonstrated pattern.

### IV. Environment-Variable Configuration

Connection credentials and endpoints MUST be supplied via environment variables
(`YDB_ENDPOINT`, `YDB_SA_KEY_FILE`). Examples MUST exit with a clear `slog.Error` message
when required variables are absent. Tuneable parameters (topic path, message count, delays)
MUST be exposed as CLI flags with sensible defaults. No credentials or endpoints MUST be
hard-coded in source files.

**Rationale**: Consistent env-var usage across all examples reduces cognitive friction and
matches standard 12-factor application practices.

### V. Structured Logging

All log output MUST use `log/slog` with a JSON handler (`slog.NewJSONHandler`). Slog MUST
be set as the default logger in `main()`. Log calls MUST include relevant structured fields
(e.g., `partition_id`, `err`, `msg_index`) rather than relying on printf-style formatting.
Final summary statistics MUST be printed to `stdout` (not via slog) in a fixed human-readable
block at the end of the demo loop.

**Rationale**: Structured JSON logs are parseable by observability tooling and teach the
correct production logging pattern. A separate plain-text stats block provides quick visual
confirmation during development.

## Technology Constraints

- **Language**: Go 1.26 (as declared in `go.mod`). No other languages are permitted in
  example source files.
- **YDB SDK**: `github.com/ydb-platform/ydb-go-sdk/v3` MUST be used for all YDB operations.
  Direct HTTP/gRPC calls bypassing the SDK are not permitted.
- **Auth**: `github.com/ydb-platform/ydb-go-yc` MUST be used for service-account
  authentication (`yc.WithServiceAccountKeyFileCredentials` + `yc.WithInternalCA()`).
- **Hashing**: Partition key routing MUST use murmur3 32-bit (`github.com/twmb/murmur3`).
  FNV or other hash algorithms MUST NOT be used for partition routing.
- **Migrations**: `goose` MUST be used for schema management. No other migration tool is
  permitted.
- **New direct dependencies**: Any new `go.mod` direct dependency MUST be noted in the
  feature plan's Technical Context section and justified against existing indirect dependencies
  before introduction.

## Development Workflow

- **Spec-first**: Every feature MUST have a `specs/<branch>/spec.md` created via
  `/speckit.specify` before implementation begins.
- **Plan-before-code**: `specs/<branch>/plan.md` MUST be produced via `/speckit.plan` and
  reviewed before any source files are created.
- **Tasks as ground truth**: `specs/<branch>/tasks.md` drives implementation; tasks MUST be
  checked off (`[X]`) as they complete. No implementation work should begin without a
  corresponding task entry.
- **Manual end-to-end validation**: The project has no automated test suite. Validation is
  performed by running `go run ./<example>/` against a live YDB instance. Each feature spec
  MUST document the expected slog output as the acceptance baseline.
- **Build gate**: `go build ./<example>/` MUST succeed before a feature branch is merged.
  The final Polish task in every `tasks.md` MUST include this build verification step.

## Governance

This constitution supersedes all informal conventions. Any amendment MUST:

1. Update this file following the versioning policy below.
2. Propagate changes to affected templates in `.specify/templates/`.
3. Update `CLAUDE.md` if the amendment affects active technology choices or commands.
4. Be recorded in the Sync Impact Report comment at the top of this file.

**Versioning policy**:

- MAJOR: Removal or redefinition of an existing principle.
- MINOR: New principle or section added.
- PATCH: Clarification, wording fix, or non-semantic refinement.

All feature plans and specs MUST include a Constitution Check section confirming compliance
or documenting justified deviations in a Complexity Tracking table. Unexplained deviations
are grounds for rejecting a plan.

**Version**: 1.0.0 | **Ratified**: 2026-03-16 | **Last Amended**: 2026-03-16
