# Research: Topic Partition Benchmark

**Branch**: `002-topic-partition-bench` | **Date**: 2026-03-16

## Decision Log

### 1. TLI Detection API

**Decision**: Use `ydb.IsOperationErrorTransactionLocksInvalidated(err)` from `github.com/ydb-platform/ydb-go-sdk/v3`.

**Rationale**: The SDK exposes a dedicated predicate for the Transaction Locks Invalidated operation
error (YDB error code 2000). Using it cleanly separates TLI counting from generic error handling.
The `query.Session.Do` method retries automatically after TLI — TLI is incremented *before*
returning to the caller, then the SDK retries the closure. To count TLIs accurately the benchmark
must detect the error inside `Do`'s closure and increment before returning `err`.

**Alternatives considered**:
- Parsing error strings — fragile, breaks on SDK updates. Rejected.
- Wrapping errors with custom sentinel — unnecessary indirection. Rejected.

**Reference**: `errors.go:88` in `ydb-go-sdk/v3` (confirmed in `03_topic/plan.md` design notes).

---

### 2. Per-Partition Reader

**Decision**: Use `db.Topic().StartReader(consumerName, topicoptions.ReadSelectors{{Path: topicPath, Partitions: []int64{partitionID}}})` for one reader per partition.

**Rationale**: Pinning each reader to a single partition ensures the 1:1 mapping between goroutine
and partition required to demonstrate that user-aligned partitioning keeps a single user's messages
on one goroutine (eliminating concurrent row-level contention). Without per-partition pinning, the
SDK's internal assignment would be non-deterministic and the benchmark would not reliably
demonstrate the contention difference.

**Alternatives considered**:
- Single shared reader across all partitions — messages from the same user could be processed
  concurrently by different goroutines, obscuring the experiment. Rejected.
- `WithPartitionRange` — not a standard SDK option; `ReadSelectors.Partitions` is the idiomatic
  approach. Rejected.

**Reference**: `read_selector.go:11` in `ydb-go-sdk/v3` (confirmed in `03_topic/plan.md` design notes).

---

### 3. Transaction Pattern for Read-Modify-Write

**Decision**: Use `db.Query().Do(ctx, func(ctx context.Context, s query.Session) error {...})` with explicit `s.Begin(ctx, query.TxSettings(query.WithSerializableReadWrite()))` + `tx.CommitTx(ctx)` inside the closure. TLI error detected at `CommitTx`, counted, then the error is returned to let `Do` retry.

**Rationale**: Per-message transactions maximize lock contention visibility — each message update is
an independent Serializable transaction, so concurrent updates to the same user row from different
goroutines produce the most TLI errors. The `Do` method provides automatic retry with backoff;
detecting TLI inside the closure lets us count every invalidation even when the retry eventually
succeeds.

**Alternatives considered**:
- `DoTx` helper — abstracts Begin/CommitTx, making it impossible to intercept TLI at commit time
  without wrapping or monkey-patching. Rejected.
- Batch transactions (multiple messages per tx) — reduces TLI frequency, hiding the contention
  signal. Rejected for the `stats` workload (used for `processed` where batch inserts are fine).

---

### 4. Stats Reset Between Scenarios

**Decision**: Execute `DELETE FROM stats` via `db.Query().Exec` between scenarios 1→3 (whenever `stats` table was used in the prior scenario) before starting the next scenario that uses `stats`.

**Rationale**: SC-004 requires that `SUM(a)+SUM(b)+SUM(c) = total_messages` after each counter
scenario. If prior rows remain, the sum will be inflated. A simple `DELETE FROM stats` resets the
table before each stats scenario.

**Alternatives considered**:
- `DROP TABLE` + recreate — requires DDL permissions at runtime and conflicts with Principle III.
  Rejected.
- Prefix user keys with scenario index — complicates queries and validation. Rejected.

---

### 5. Message Count Stop Condition

**Decision**: Use `sync/atomic.Int64` shared counter incremented by each goroutine after successful
message processing. Cancel the scenario context when the counter reaches the target.

**Rationale**: Each goroutine processes batches independently; a shared atomic counter is the
simplest correct way to detect when exactly `N` messages have been processed across all partitions
without coordination overhead or channels.

**Alternatives considered**:
- Per-partition message counters summed at the end — does not allow early context cancellation
  during the run. Rejected.
- Channel-based counting — adds goroutine and channel lifecycle complexity. Rejected.

---

### 6. No New Direct Dependencies

**Decision**: All required functionality is covered by existing `go.mod` direct dependencies. No
new direct dependencies are introduced.

**Rationale**: `uuid` (message IDs), `murmur3` (partition routing), `ydb-go-sdk/v3` (all YDB
operations), `ydb-go-yc` (auth) are already present. Standard library provides `log/slog`,
`sync/atomic`, `context`, `flag`, `os/signal`, `time`, `encoding/json`, `fmt`, `errors`.

---

### 7. Multi-File `package main` vs Single main.go

**Decision**: Split `03_topic/` into `main.go`, `producer.go`, `consumer.go`, `message.go` — all in `package main`.

**Rationale**: Principle I requires a single `main.go` for logic, but the benchmark is
significantly more complex than previous examples (4 scenarios, two producers, 10 goroutines,
TLI tracking). Splitting into multiple `package main` files within the same directory keeps each
file focused and readable while satisfying `go run ./03_topic/` (Go compiles all `.go` files in
the directory as one package). No sub-packages are introduced; import paths remain unchanged.

**Alternatives considered**:
- Single 500+ line main.go — technically compliant but unreadable. Rejected on readability grounds.
- Sub-packages (`03_topic/producer/`, etc.) — violates Principle I explicitly. Rejected.
