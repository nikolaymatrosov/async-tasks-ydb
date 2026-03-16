# Data Model: Example 03 — Direct Topic Writer

**Phase**: 1 — Design & Contracts
**Date**: 2026-03-16

## Entities

### Producer

Owns the full lifecycle of all per-partition writers for a single topic.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `db` | `*ydb.Driver` | Open YDB driver; used for `Topic().Describe()` and `Topic().StartWriter()` |
| `topic` | `string` | Absolute topic path (`db.Name() + "/" + suffix`) |
| `partitions` | `[]int64` | Ordered slice of active partition IDs; index is the hash target |
| `writers` | `map[int64]*safeWriter` | Per-partition writer keyed by partition ID |

**State transitions**:

```
Unstarted ──Start()──▶ Running ──Stop()──▶ Stopped
                                  │
                               Write() available only in Running state
```

**Invariants**:
- `writers == nil` in Unstarted state; calling `Start()` twice panics.
- `len(partitions) == len(writers)` after a successful `Start()`.

---

### safeWriter

Thin wrapper around a single `topicwriter.Writer` that adds retry logic.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `w` | `*topicwriter.Writer` | Underlying SDK writer pinned to one partition |

**Behaviour**:
- `Write(ctx, messages, logErr)`: runs exponential-backoff retry loop; returns only when all messages are acknowledged or an unretriable error occurs.
- `Close(ctx)`: delegates to `w.Close(ctx)`.

---

### Message (topicwriter.Message)

Provided by the YDB SDK. An `io.Reader`-backed value carrying the message payload.

| Attribute | Notes |
| --------- | ----- |
| Data | `io.Reader` — the message body bytes |
| SeqNo | Set by SDK automatically when zero |
| CreatedAt | Set to current time by SDK when zero |

The example creates messages with `topicwriter.Message{Data: strings.NewReader(payload)}`.

---

### PartitionKey

A plain `string` value passed by the caller to `Producer.Write`. It is never written to the topic; it is used only to select which `safeWriter` to target.

**Routing rule**: `partitionIndex = FNV1a64(key) % len(partitions)` → `partitionID = partitions[partitionIndex]`.

---

## Backoff Parameters

| Parameter | Value | Source |
| --------- | ----- | ------ |
| Initial interval | 1 second | standard exponential start |
| Multiplier | 1.5× | gentle ramp |
| Max interval | 30 seconds | mirrors reference |
| Max elapsed time | 5 minutes | mirrors reference |
| Queue-full retry | indefinite (no elapsed cap) | mirrors reference |

---

## No Persistent Storage

This example writes to a YDB topic (append-only message stream). It does not read from or write to any YDB table. There is no schema migration required beyond creating the target topic once.
