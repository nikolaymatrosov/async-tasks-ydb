# Research: Example 03 — Direct Topic Writer

**Phase**: 0 — Outline & Research
**Date**: 2026-03-16

## Findings

### 1. YDB Topic Writer API

**Decision**: Use `db.Topic().Describe()` to enumerate partitions, then `db.Topic().StartWriter()` with `topicoptions.WithWriterPartitionID` to pin each writer to a specific partition.

**Rationale**: The reference implementation (`docs/producer.go`) demonstrates exactly this pattern. The YDB Go SDK v3 exposes per-partition writers via `topicoptions.WithWriterPartitionID(partitionID)`. Pinning is required for ordering guarantees — without it, the SDK may round-robin across partitions internally.

**Alternatives considered**:

- Using a single un-pinned writer: rejected — does not guarantee key-to-partition consistency.
- Using a hash ring against partition count without `Describe`: rejected — partition count may change; `Describe` gives the live active set.

---

### 2. Partition Key Routing — FNV-1a Hash

**Decision**: Implement FNV-1a (64-bit) hash on the partition key string, take `abs(hash) % len(activePartitions)` as the index into a stable slice of partition IDs.

**Rationale**: FNV-1a is simple, dependency-free, deterministic across runs, and already proven in the reference implementation. The 64-bit variant avoids clustering on short keys that the 32-bit variant can exhibit.

**Alternatives considered**:

- `crc32`: equally simple but less uniform for short ASCII strings.
- `xxhash` / `murmur3`: better distribution, but require an external dependency — violates the no-new-deps constraint.

---

### 3. Retry Strategy

**Decision**: Wrap each write in `backoff.Retry` from `github.com/cenkalti/backoff/v5` — this package is already present in `go.mod` as an indirect dependency.

**Resolution**: Use `backoff.Retry(ctx, operation, backoff.WithBackOff(b), backoff.WithMaxElapsedTime(d))` from `github.com/cenkalti/backoff/v5`. Constants: max interval 30 s, max elapsed 5 min. For `ErrQueueLimitExceed`, restart the outer retry loop unconditionally (matching reference behaviour).

**Rationale**: `cenkalti/backoff/v5` is already in `go.mod`; no new dependency needed. The v5 API is generic and context-aware — context is the first argument to `Retry`, `WithContext` is gone, and `MaxElapsedTime` is expressed via the `WithMaxElapsedTime` RetryOption.

**Alternatives considered**:

- Adding `cenkalti/backoff/v4` as a dependency: superseded by v5 which is already present.
- `golang.org/x/sync`: not relevant to retry logic.

---

### 4. Server-Ack Semantics

**Decision**: Pass `topicoptions.WithWriterWaitServerAck(true)` when creating each partition writer.

**Rationale**: Without this option, `Write` returns as soon as the message is queued locally. The spec requires server acknowledgement before `Write` returns (FR-004). The reference implementation uses this option for the same reason.

---

### 5. Error Classification

**Decision**: Classify errors as retriable if `ydb.IsTransportError(err)` returns true; treat `topicwriter.ErrQueueLimitExceed` as a special infinite-retry case; treat all other errors as permanent.

**Rationale**: Mirrors the reference `WithWriterCheckRetryErrorFunction`. Transport errors (network blips, gRPC resets) are transient. Application-level errors (auth, schema mismatch) are permanent and should surface immediately.

---

### 6. Topic Path

**Decision**: Construct the full topic path as `db.Name() + "/" + topicSuffix`, where the suffix defaults to a flag value. This matches the convention in `02_cdc_worker/main.go`.

**Rationale**: `db.Name()` returns the database root path (e.g., `/ru-central1/b1g.../etn.../`). Topic paths in YDB are always absolute. Concatenating with a relative suffix is the established pattern in this project.

---

### 7. Topic for the Example

**Decision**: Re-use an existing topic or create a dedicated `tasks/direct` topic via a new migration. For the example binary, the topic path is supplied as a CLI flag (default `tasks/direct`) — no hard-coded paths.

**Rationale**: The existing `tasks/cdc_tasks` changefeed topic is managed by the YDB engine and is not writable by user code. A separate topic is needed. The example README will document the one-time setup step (`ydb topic create`).

---

## Summary Table

| Area | Decision |
| ---- | -------- |
| Partition enumeration | `db.Topic().Describe()` at startup |
| Partition pinning | `topicoptions.WithWriterPartitionID` per writer |
| Routing algorithm | FNV-1a 64-bit, `abs(hash) % len(partitions)` |
| Retry library | `cenkalti/backoff/v5` (already in go.mod as indirect dep) |
| Ack mode | `WithWriterWaitServerAck(true)` |
| Error classification | `ydb.IsTransportError` + `ErrQueueLimitExceed` special case |
| Topic path convention | `db.Name() + "/" + flag` |
| Demo topic | New `tasks/direct` topic, setup documented in README |
