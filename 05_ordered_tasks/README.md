# 05_ordered_tasks — Per-Entity Ordered Task Delivery

Forked from `04_coordinated_table/`, this example demonstrates **strict
per-entity FIFO** dispatch on top of the coordinated-task pattern, with
parallelism preserved across entities.

Key differences from `04_coordinated_table`:

- **`priority` and `hash` columns are dropped** — ordering is the only
  scheduling axis.
- Each task carries `entity_id` (domain identifier) and `entity_seq` (synthetic
  monotonic ordinal).
- `partition_id = murmur3.Sum32(entity_id) % partitions` — same entity always
  routes to the same partition.
- The producer is **single-instance**. It writes directly to the
  `ordered_tasks` table; **no topic, no relay, no transaction**. The seq
  generator is `UnixNano()*1024 + atomic_tiebreaker`, strictly increasing
  process-wide and across restarts.
- The worker dispatches only the **head** (smallest non-terminal `entity_seq`)
  per entity per scan. Successors stay invisible during in-flight processing,
  retry backoff, and terminal failure.
- Status vocabulary: `pending`, `locked`, `completed`, `failed`, `skipped`.
  Backoff is encoded on the head row's `scheduled_at`. Terminal failure
  (`status='failed'`) blocks the entity until an operator resolves it.

## Components

```text
cmd/producer        single-instance batch generator (rate-shaped)
cmd/worker          per-partition head-of-entity dispatcher
pkg/taskproducer    nextSeq() + buildBatch + AS_TABLE upsert
pkg/taskworker      Candidate / ClaimedTask + repository + dispatch loop
pkg/rebalancer      256 partition exclusive semaphores via YDB coordination
pkg/ydbconn         YDB driver factory (env-var creds)
pkg/uid             UUID v4 helper
pkg/metrics         Prometheus + slog stats; entity-aware fields
```

## Quickstart

See [`specs/017-entity-task-ordering/quickstart.md`](../specs/017-entity-task-ordering/quickstart.md)
for the full end-to-end validation flow. Summary:

```bash
# 1. apply migration
go run ./cmd/migrate up

# 2. test target (no faults)
go run ./06_target_server --listen :8080 --metrics-port :9091

# 3. worker
go run ./05_ordered_tasks/cmd/worker \
  --partitions 256 \
  --lock-duration 5s \
  --backoff-min 50ms --backoff-max 5s \
  --max-attempts 10 \
  --metrics-port 9090

# 4. producer
go run ./05_ordered_tasks/cmd/producer \
  --rate 500 \
  --partitions 256 \
  --batch-window 250ms \
  --apigw-url localhost:8080 \
  --entities 1000
```

## Flags

### `cmd/producer`

| Flag | Default | Description |
|---|---|---|
| `--rate` | 100 | tasks/sec |
| `--partitions` | 256 | logical partitions |
| `--batch-window` | 100ms | batch accumulation window |
| `--apigw-url` | `$APIGW_URL` | host put into each task's `payload.url` |
| `--entities` | 1000 | size of synthetic entity pool (`entity-0000000`..) |
| `--metrics-port` | 9090 | Prometheus `/metrics` |

### `cmd/worker`

| Flag | Default | Description |
|---|---|---|
| `--partitions` | 256 | logical partitions |
| `--lock-duration` | 5s | per-task lease |
| `--backoff-min` | 50ms | initial backoff |
| `--backoff-max` | 5s | max backoff |
| `--max-attempts` | 10 | retries before terminal failure |
| `--fetch-k` | 32 | rows per eligibility scan |
| `--metrics-port` | 9090 | Prometheus `/metrics` |

## Worker → target HTTP headers

The worker POSTs each task's `payload.url` with these headers:

```text
Content-Type:  application/json
X-Task-ID:     <uuid>
X-Entity-ID:   <task.EntityID>
X-Entity-Seq:  <decimal uint64>
```

The test target server in `06_target_server/` reads `X-Entity-{ID,Seq}` and
flags any out-of-order arrival as a `kind=rewind` violation.

## Operator-resolution recipe

When `attempt_count` reaches `--max-attempts`, the worker writes
`status='failed'` on the head row. The entity stays blocked (successors are
invisible) until an operator clears it manually:

```sql
UPDATE ordered_tasks
SET status      = 'skipped',
    resolved_by = 'operator-<name>',
    resolved_at = CurrentUtcTimestamp()
WHERE entity_id = '<entity-id>'
  AND status    = 'failed';
```

`'skipped'` is also terminal, so the next non-terminal `entity_seq` for that
entity becomes the new head and is dispatched on the next scan cycle.

## Schema

See [`migrations/20260429000007_create_ordered_tasks.sql`](../migrations/20260429000007_create_ordered_tasks.sql).
PK is `(partition_id, id)`; the worker's head-of-entity scan is served by the
covering global index `idx_partition_entity_seq ON (partition_id, entity_id, entity_seq)`.
