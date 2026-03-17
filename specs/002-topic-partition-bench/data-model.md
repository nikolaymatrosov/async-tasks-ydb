# Data Model: Topic Partition Benchmark

**Branch**: `002-topic-partition-bench` | **Date**: 2026-03-16

## In-Memory Entities

### BenchMessage

Represents a single benchmark payload generated before publishing begins.

```go
type BenchMessage struct {
    ID     uuid.UUID // uuid.New() — unique per message
    UserID  uuid.UUID // drawn from UserIDSampler — identifies the "owner"
    Type   string // one of "A", "B", "C" — simulates message category
}
```

**Validation rules**:

- `ID` must be non-empty UUID string
- `UserID` must be one of the N pre-generated user IDs from `UserIDSampler`
- `Type` must be exactly `"A"`, `"B"`, or `"C"`

**Lifecycle**: Generated in bulk before publishing. Immutable after creation. Serialized to JSON
for topic transport; deserialized by consumers.

**JSON wire format**:

```json
{"id":"<uuid>","user_id":"<uuid>","type":"A"}
```

---

### ScenarioResult

Metrics collected for one benchmark scenario.

```go
type ScenarioResult struct {
    Name      string        // e.g. "by_user → stats"
    Messages  int64         // total messages consumed
    TLIErrors int64         // transaction lock invalidation count
    Duration  time.Duration // wall-clock time for consumption phase
    MsgPerSec float64       // Messages / Duration.Seconds()
}
```

**Computed field**: `MsgPerSec = float64(Messages) / Duration.Seconds()`

---

### UserIDSampler (existing — utils.go)

Pre-generates N UUID strings and assigns weights using a random power-law distribution. Provides
O(log N) sampling. Already implemented and tested.

```
Fields: ids []string, cumWeights []float64
Operations: Sample() string, IDs() []string, All() iter.Seq[Entry]
```

---

## YDB Schema

### Topic: `tasks/by_user`

Messages partitioned by `UserID` using murmur3 routing. Each user's messages land on the same
partition, so a single consumer goroutine handles all updates for that user.

| Property | Value |
|----------|-------|
| Partitions | 10 (min_active) |
| Retention | 24 hours |
| Consumers | `bench-byuser-stats`, `bench-byuser-processed` |

---

### Topic: `tasks/by_message_id`

Messages partitioned by `ID` (unique per message). Each message lands on a pseudo-random
partition — the same user's messages are spread across all 10 partitions.

| Property | Value |
|----------|-------|
| Partitions | 10 (min_active) |
| Retention | 24 hours |
| Consumers | `bench-bymsgid-stats`, `bench-bymsgid-processed` |

---

### Table: `stats`

Stores per-user counters. Updated via read-modify-write transactions — this is the contention-prone
workload. Reset between scenarios that write to it.

| Column | Type | Constraints |
|--------|------|-------------|
| `user_id` | `UUID` | NOT NULL, PRIMARY KEY |
| `a` | `Int64` | nullable — count of type "A" messages |
| `b` | `Int64` | nullable — count of type "B" messages |
| `c` | `Int64` | nullable — count of type "C" messages |

**Access pattern**: `SELECT a, b, c WHERE user_id = $uid` → increment → `UPSERT`. One transaction
per message. Serializable isolation.

**Correctness invariant**: After each stats scenario completes,
`SELECT SUM(a) + SUM(b) + SUM(c) FROM stats` MUST equal the total message count (SC-004).

**Reset**: `DELETE FROM stats` executed between stats scenarios to ensure independence.

---

### Table: `processed`

Records which message IDs have been consumed. Insert-only workload — no read-modify-write,
so no contention is expected regardless of partitioning strategy.

| Column | Type | Constraints |
|--------|------|-------------|
| `id` | `UUID` | NOT NULL, PRIMARY KEY |

**Access pattern**: `UPSERT INTO processed (id) VALUES ($id)`. No preceding read.

**Note**: UPSERT is idempotent; duplicate inserts are safe. Used to demonstrate near-zero TLI.

---

## State Transitions

```
Program start
  └─ Generate N BenchMessages (in-memory)
  └─ Publish to by_user topic   (keyed by UserID)
  └─ Publish to by_message_id   (keyed by ID)

Scenario 1: by_user → stats
  └─ Consume from by_user/bench-byuser-stats
  └─ For each msg: READ stats[user_id] → UPDATE → COMMIT (count TLI)
  └─ Verify: SUM(a+b+c) = N
  └─ Reset: DELETE FROM stats

Scenario 2: by_user → processed
  └─ Consume from by_user/bench-byuser-processed
  └─ For each msg: UPSERT INTO processed

Scenario 3: by_message_id → stats
  └─ Consume from by_message_id/bench-bymsgid-stats
  └─ For each msg: READ stats[user_id] → UPDATE → COMMIT (count TLI)
  └─ Verify: SUM(a+b+c) = N
  └─ Reset: DELETE FROM stats

Scenario 4: by_message_id → processed
  └─ Consume from by_message_id/bench-bymsgid-processed
  └─ For each msg: UPSERT INTO processed

Print comparison table → exit
```
