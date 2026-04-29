# Phase 0 Research: Per-Entity Ordered Task Delivery

**Feature**: 017-entity-task-ordering
**Date**: 2026-04-29 (revised after `/speckit.clarify` session same day)

This document resolves the open design questions raised by the spec's functional requirements
(FR-001 … FR-021) and the plan's Technical Context. Each section follows
**Decision / Rationale / Alternatives considered**.

---

## 1. How is "the head task per entity" represented and queried?

**Decision**: The new table `ordered_tasks` carries `entity_id Utf8 NOT NULL` and `entity_seq
Uint64 NOT NULL`. The head of an entity is the row with the **smallest `entity_seq`** among
non-terminal rows for that `entity_id`. A row reaches a terminal state when `status IN
('completed', 'failed', 'skipped')`.

The worker's eligibility query becomes (sketch, executed per partition):

```sql
DECLARE $partition_id AS Uint16;
DECLARE $k AS Uint64;
SELECT id, entity_id, entity_seq, payload, status, scheduled_at, locked_until, attempt_count
FROM ordered_tasks VIEW idx_partition_entity_seq    -- secondary index, see §3
WHERE partition_id = $partition_id
  AND status IN ('pending', 'locked')
ORDER BY entity_id, entity_seq
LIMIT $k;
-- worker then keeps, for each distinct entity_id seen, only the FIRST row,
-- discards the rest, and attempts to claim that head — see §4.
```

Backoff and terminal failure are encoded on the head row itself (`status='pending' AND
scheduled_at=retry_at` after a transient failure; `status='failed'` after retries exhausted).
Because a backed-off head is still the smallest non-terminal `entity_seq`, the dedup-by-entity
step still picks it, and the worker then sees `scheduled_at > now` and skips it — keeping later
seqs invisible for the full backoff window (FR-006).

**Rationale**: Storing the order in the row itself (no auxiliary index table, no topic, no relay)
keeps the design at the absolute minimum: a single new table and a single goose migration.

**Alternatives considered**:

- **`entity_queue_state` index table**: rejected as over-engineered for the demo workload.
- **Topic + relay design** (previous draft of this document): rejected per `/speckit.clarify`
  session 2026-04-29 — the user wants a simpler, single-process producer that writes directly to
  the table.
- **YDB coordination semaphores per entity**: rejected; the rebalancer already uses 256 partition
  semaphores and per-entity semaphores would not scale.

---

## 2. How does the producer assign `entity_seq`?

**Decision**: The producer is **single-instance** (per Clarifications). For each task it generates,
it computes:

```go
// Process-wide strictly-increasing source: nanosecond clock + atomic tiebreaker.
// monotonic across the producer's lifetime regardless of clock resolution.
seq := uint64(time.Now().UnixNano())*1024 + atomic.AddUint64(&seqCounter, 1)
```

This is the synthetic, opaque `entity_seq`. Properties:

- **Strictly increasing per process**: every `seq` value the producer ever emits is larger than
  the previous one (the `1024×UnixNano` term dominates over the atomic; the atomic resolves
  same-nanosecond ties).
- **Monotonic per entity** by transitivity: any two tasks for the same `entity_id` are generated
  one-after-the-other in the producer's loop, so the later one always gets a larger seq.
- **Monotonic across batches**: even if the producer regenerates the same `entity_id` in a later
  batch (or after process restart, where the `time.Now().UnixNano()` keeps advancing), the new
  seq is strictly greater than any previously-emitted one for that entity, so the head-of-entity
  invariant still holds.
- **Sparse per entity**: between any two of an entity's seqs, other entities' seqs may sit. The
  worker's "smallest non-terminal seq" predicate doesn't care about contiguity.
- **No persisted producer state**: the producer forgets the mapping immediately after the batch
  upsert returns success — exactly the behaviour the user described.

The producer's batch upsert path becomes:

```go
// in pkg/taskproducer/producer.go
for each task in batch:
    entity_id := pool[rand.Intn(N)]                      // synthesised pool, see Clarifications
    seq       := nextSeq()                               // strictly-increasing process-wide
    partition := uint16(murmur3.Sum32([]byte(entity_id)) % partitions)
    rows = append(rows, taskRow{id, entity_id, seq, partition, payload, ...})

UPSERT INTO ordered_tasks SELECT ... FROM AS_TABLE($records)
```

No SELECT-MAX, no serializable transaction, no per-entity counter map.

**Rationale**:

- Trivially correct under the single-producer assumption (FR-002 only needs strict per-entity
  monotonicity; this design provides strict global monotonicity, which is strictly stronger).
- Survives producer restart: `time.Now().UnixNano()` advances; the next launched producer's seqs
  are larger than the previous run's even if it picks up the same entity_ids.
- No round-trip overhead on the hot path; producer's existing rate-shaping/batch-window logic is
  preserved unchanged.
- Truly stateless wrt entities — exactly the user's "forgets it immediately after data is written
  to db" directive.

**Alternatives considered**:

- **Per-entity in-memory counter map**: requires startup recovery (read MAX from table per
  entity) to avoid colliding with previously-written rows, contradicting the user's "forgets
  immediately" directive. Rejected.
- **Topic-partition offset as seq** (previous draft): rejected per `/speckit.clarify` —
  unnecessary complexity for a single-instance producer.
- **Pure atomic counter (no clock term)**: would reset to 0 on restart and could collide with
  previously-written rows. Rejected.

**Note on multi-producer**: This scheme breaks if two producers run concurrently — their atomic
counters and clocks are independent, so two concurrent emissions for the same entity could land
with seqs in arbitrary order. This is explicitly out of scope for this fork (Clarifications,
Edge Cases section).

---

## 3. Index strategy for the head-of-entity SELECT

**Decision**: Add a global secondary index in the same migration that creates the table:

```text
INDEX idx_partition_entity_seq GLOBAL ON (partition_id, entity_id, entity_seq)
  COVER (id, payload, status, scheduled_at, locked_until, attempt_count)
```

The worker's per-partition scan reads `partition_id = $pid AND status IN ('pending','locked')`,
ordered by `(entity_id, entity_seq)`, taking the first row of each `entity_id` group from a
covering read. The point-`UPDATE` to claim still hits the base table by primary key.

**Rationale**: Without the index, the PK `(partition_id, id)` does not order by `entity_seq`; the
worker would have to scan all open rows in the partition and post-filter, breaking SC-003 (≤ 10 %
overhead on unblocked entities) at high backlog.

---

## 4. Handling multiple eligible entities in one partition (worker dispatch loop)

**Decision**: Replace today's `LIMIT 1` snapshot SELECT with a `LIMIT K` (configurable, default
`K = 32`) snapshot SELECT and a **client-side dedup-by-entity step**:

1. SELECT up to `K` rows ordered by `(entity_id, entity_seq)`.
2. Walk the rows in order; for each new `entity_id`, take the first row as that entity's head and
   discard subsequent rows for the same entity.
3. From the resulting head set, drop entries whose `scheduled_at > now` (entity is in backoff)
   and entries whose `status='locked' AND locked_until > now` (claimed by someone else and lease
   not yet expired).
4. Pick one head from the survivors (round-robin or random across entities to avoid starvation
   when multiple entities are simultaneously eligible) and attempt `ClaimTask` on it.
5. If the claim CAS-fails (another worker iteration changed the row), fall back to the next head.

This preserves FR-003 (only one in-flight task per entity at a time): the head row is locked with
a `lock_value`, and the next eligibility scan still sees it as the smallest non-terminal seq for
that entity, keeping later seqs invisible.

**Rationale**: The current single-row SELECT can't distinguish "head of entity A, in backoff"
from "head of entity B, dispatchable" without seeing both. K = 32 is conservative — small enough
that the read is cheap, large enough to almost always find a dispatchable entity.

---

## 5. Backoff representation: how does a backed-off head block its successors?

**Decision**: Use the `scheduled_at` column on the head row to encode the retry deadline. When
the worker's `ProcessTask` callback returns a transient error:

- Worker computes `retry_at = now + nextBackoffDelay()` (uses existing `BackoffMin..BackoffMax`
  exponential sequence, but applied to the *task* rather than the loop).
- Worker calls `MarkFailedWithBackoff(ctx, claimed, retry_at, lastError)` which, in one
  serializable tx, updates the row to `status='pending', lock_value=NULL, locked_until=NULL,
  scheduled_at=retry_at, attempt_count=attempt_count+1, last_error=lastError`.

The dispatch step in §4 includes the head row in its scan (because the eligibility predicate is
`status IN ('pending','locked')`, with no `scheduled_at` filter), but skips heads whose
`scheduled_at > now`. Later seqs for the entity are therefore *not* picked, because they are not
the entity's smallest seq — the backed-off head still is. (FR-006)

**Rationale**: Reuses an existing column from the design and avoids a new "blocked_until" column
or sidecar table.

---

## 6. Partition routing key

**Decision**: Hash by `entity_id`. Specifically, `partition_id =
uint16(uint64(murmur3.Sum32([]byte(entity_id))) % uint64(partitions))` — both at producer write
time (it's the producer that writes the row directly) and conceptually at any future component
that needs to know an entity's partition.

**Rationale**: Per-entity ordering is enforced *within a partition* (the rebalancer leases
partitions, so at any moment only one worker is dispatching a given partition). If two tasks for
the same entity could land on different partitions, two workers could dispatch them concurrently
and the order guarantee breaks. Hashing by `entity_id` is the structural invariant that makes
"head of entity = head of entity within its single partition" sound.

The `hash` column from the original `coordinated_tasks` table is **dropped** in this fork — it
was only used for diagnostics and is redundant with `entity_id` for routing.

---

## 7. Terminal-failure and retry-exhaustion semantics

**Decision**: Add `attempt_count Uint32 NOT NULL DEFAULT 0` and a configurable `--max-attempts`
flag on the worker (default `10`). On each `MarkFailedWithBackoff`, increment `attempt_count`.
When `attempt_count >= max_attempts`, the worker calls `MarkTerminallyFailed(ctx, claimed,
last_error_string, failed_at)` which sets `status='failed'`, writes the failure into
`last_error`, and stamps `done_at`.

`'failed'` is **not** in the eligibility predicate, so the entity remains blocked (the head row
in `failed` state still has the smallest `entity_seq` for that entity; the dedup step picks it
but then sees a non-eligible status — the entity is silently skipped from dispatch). The blocked
state is observable via `/state` (worker metrics) and via direct YDB queries.

Operator resolution path (FR-009): set `status='skipped'`, `resolved_by`, `resolved_at` on the
head row. `'skipped'` is also a terminal status excluded from the eligibility predicate; the
*next* seq for that entity is now the smallest non-terminal seq and becomes the new head.

---

## 8. At-least-once safety: duplicate completion / failure reports

**Decision**: All status transitions out of `locked` are conditioned on the **current**
`(status, lock_value)` of the row, written in a serializable transaction:

- `MarkCompleted(...)` requires `status='locked' AND lock_value=$lv`; otherwise no-op.
- `MarkFailedWithBackoff(...)` requires `status='locked' AND lock_value=$lv`; otherwise no-op.
- `MarkTerminallyFailed(...)` requires `status='locked' AND lock_value=$lv`; otherwise no-op.

This makes duplicate reports idempotent (FR-011): the second report finds the row no longer in
`locked` state with that `lock_value` and exits cleanly.

---

## 9. Lease loss / consumer-crash reconciliation (FR-010)

**Decision**: Continue to rely on `locked_until` expiry. When `now > locked_until` and
`status='locked'`, the eligibility scan in §4 surfaces the row (predicate is `status IN
('pending','locked')`), the dedup step picks it as the head, and the claim CAS re-locks it under
a new `lock_value`. The row is still the smallest `entity_seq` for the entity, so the head
remains the head — no later seq is released.

No new code is needed for this path beyond the existing `ClaimTask` logic copied from
`04_coordinated_table`.

---

## 10. Test target server: ordinal-state representation

**Decision**: A `sync.Mutex`-guarded (sharded by entity hash) `map[string]uint64` keyed by
`entity_id` storing `last_accepted_seq`. Because `entity_seq` is a synthetic monotonic value
(strictly increasing per producer process), it is **monotonic but sparse per entity**. The check
uses strict-greater:

- `received_seq > last_accepted_seq` → accept, set `last = received`, 200 OK.
- `received_seq == last_accepted_seq` → idempotent duplicate (FR-017), 200 OK, no counter
  change, no violation.
- `received_seq < last_accepted_seq` → **rewind violation** (FR-016): emit
  `slog.Warn("ordering violation", entity_id, last_accepted_seq, received_seq, kind="rewind")`
  and increment `ordering_violation_total{bucket}`. Respond 200 OK so the worker doesn't retry
  the violation itself into a loop.
- If a fault is injected (§11), short-circuit the ordinal logic and respond with the injected
  status; do not advance the counter, do not count as a violation.

A first-time entity has implicit `last_accepted_seq = 0`; the producer's first emitted seq
(always > 0 because it embeds `UnixNano`) is accepted.

**Cardinality control on the violation metric**: the metric is labelled by a fixed-size hash
bucket of `entity_id` (`bucket = murmur3.Sum32(entity_id) % 64`), not the raw entity id —
preventing cardinality explosion under high entity counts. The structured log entry carries the
full `entity_id` (FR-016).

---

## 11. Test target server: fault injection

**Decision**: At startup, the server reads `--fault-429-percent` (0..100) and `--fault-5xx-percent`
(0..100). It validates `fault_429 + fault_5xx ≤ 100` and exits with a clear slog error otherwise
(FR-018). On each request:

```go
roll := rand.Intn(100)
switch {
case roll < fault429:
    respond(429); return
case roll < fault429+fault5xx:
    respond(503); return
default:
    handleIngest(...)
}
```

Per-request uniform random (FR-019). 429 and 503 are both in the worker's transient-error path,
so they cleanly drive `MarkFailedWithBackoff` (FR-021).

---

## 12. Worker → target server URL plumbing

**Decision**: The producer encodes the destination as `payload = {"url": "https://${apigwURL}/"}`
exactly as `04_coordinated_table` does. The worker POSTs to `payload.url`. To attach ordering
metadata, the worker adds two HTTP headers from the just-locked `ClaimedTask`:

```go
req.Header.Set("X-Entity-ID",  task.EntityID)
req.Header.Set("X-Entity-Seq", strconv.FormatUint(task.EntitySeq, 10))
```

Production endpoints would simply ignore these headers; the test target reads them.

---

## 13. Migration shape

**Decision**: A single new goose migration creates the `ordered_tasks` table and its secondary
index in one Up + symmetric Down:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE ordered_tasks (
    id            Utf8         NOT NULL,
    partition_id  Uint16       NOT NULL,
    entity_id     Utf8         NOT NULL,
    entity_seq    Uint64       NOT NULL,
    status        Utf8         NOT NULL,
    payload       Utf8         NOT NULL,
    lock_value    Utf8,
    locked_until  Timestamp,
    scheduled_at  Timestamp,
    attempt_count Uint32       NOT NULL,
    last_error    Utf8,
    resolved_by   Utf8,
    resolved_at   Timestamp,
    created_at    Timestamp    NOT NULL,
    done_at       Timestamp,
    PRIMARY KEY (partition_id, id),
    INDEX idx_partition_entity_seq GLOBAL ON (partition_id, entity_id, entity_seq)
      COVER (id, payload, status, scheduled_at, locked_until, attempt_count)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ordered_tasks;
-- +goose StatementEnd
```

Fresh table → no backfill, no nullable-column compromise. PK is `(partition_id, id)` — `priority`
is gone and `id` alone is unique enough to disambiguate within a partition.

---

## Summary of resolved unknowns

| Question | Answer |
| --- | --- |
| Where does this feature live? | New top-level example `05_ordered_tasks/` (forked from `04_coordinated_table/`) |
| Is `priority` retained? | No — dropped from the schema in this fork |
| Where does per-entity order live? | In `ordered_tasks.entity_seq`, indexed by `(partition_id, entity_id, entity_seq)` |
| How is seq assigned? | Single-instance producer assigns `seq = UnixNano()*1024 + atomic_tiebreaker`; opaque, strictly increasing globally; forgotten after the upsert |
| How does backoff block successors? | Head row's `scheduled_at` set to retry deadline; dedup step skips entities whose head is in the future |
| Terminal failure? | `status='failed'` after `attempt_count >= max_attempts`; operator resolves via `status='skipped'` |
| Partition routing key? | `entity_id` — `partition_id = murmur3(entity_id) % partitions` |
| Topic / relay? | None — producer writes directly to the table |
| Target server check? | `received > last_accepted` = accept; `==` = duplicate; `<` = rewind violation |
| Target server ordinal state? | In-memory `map[entity_id]uint64`, lost on restart |
| Fault injection? | Per-request uniform random over `[0,100)`, two split rates, sum ≤ 100 |
| Worker → target wiring? | Producer-supplied `payload.url`, plus new `X-Entity-ID` / `X-Entity-Seq` headers |
| Migration shape? | Single goose Up creating new table + global covering index; symmetric Down |

No NEEDS CLARIFICATION items remain.
