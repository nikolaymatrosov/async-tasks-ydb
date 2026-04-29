# Quickstart: Per-Entity Ordered Task Delivery

**Feature**: 017-entity-task-ordering

End-to-end manual validation against a live YDB instance. This is the acceptance baseline
referenced in the constitution (no automated test suite).

## Prerequisites

- Go 1.26 toolchain (matches `go.mod`).
- A reachable YDB Serverless or Dedicated database.
- `YDB_ENDPOINT`, `YDB_DATABASE`, `YDB_SA_KEY_FILE` exported in the shell.
- `goose` migration tool already wired through `cmd/migrate/main.go`.

## 1. Apply the migration

```bash
go run ./cmd/migrate up
```

Expected slog line:

```text
{"level":"INFO","msg":"goose up","migration":"20260429000007_create_ordered_tasks.sql","status":"OK"}
```

Verify in YDB Console (or `ydb scheme describe`): a new table `ordered_tasks` with a global
covering index `idx_partition_entity_seq` exists. The original `coordinated_tasks` is unchanged.

## 2. Start the test target server

```bash
# Healthy run (no fault injection): used for SC-001, SC-007.
go run ./06_target_server --listen :8080 --metrics-port :9091
```

Expected slog at startup:

```text
{"level":"INFO","msg":"target server starting","listen":":8080","fault_429_percent":0,"fault_5xx_percent":0}
```

## 3. Start one or more workers

The worker reads its destination from each task's `payload.url` (set by the producer in step 4),
so it takes no destination flag.

```bash
go run ./05_ordered_tasks/cmd/worker \
  --partitions 256 \
  --lock-duration 5s \
  --backoff-min 50ms --backoff-max 5s \
  --max-attempts 10 \
  --metrics-port 9090
```

## 4. Start the (single-instance) producer

```bash
go run ./05_ordered_tasks/cmd/producer \
  --rate 500 \
  --partitions 256 \
  --batch-window 250ms \
  --apigw-url localhost:8080 \
  --entities 1000
```

Notes:

- The producer is **single-instance** for this fork (Clarifications). Running two of them
  concurrently against the same database will violate the per-entity ordering guarantee.
- `--entities N` synthesises an entity pool `entity-0000000` … `entity-0000(N-1)` and picks
  uniformly per task.
- `entity_seq` is a synthetic, opaque, monotonically-increasing value generated per task
  (`UnixNano()*1024 + atomic_tiebreaker`). The producer forgets it after the upsert returns.

## 5. Validate User Story 1 — strict in-order delivery (SC-001)

Let the system run for ~60 seconds at the rate above (≈ 30 000 tasks across 1 000 entities). Then:

- `curl -s localhost:9091/state | jq '.totals'` — confirm `violation_total == 0`.
- Tail worker stdout: every `apigw call` log line ends with `"http_status":200`.
- Sample a single entity:

  ```bash
  curl -s 'localhost:9091/state?top=1000' \
    | jq '.top_entities[] | select(.entity_id=="entity-0000042")'
  ```

  Its `last_accepted_seq` strictly increases over time with no rewinds. The offsets are sparse
  (gaps between this entity's events are normal — `entity_seq` is a global counter, not
  per-entity).

Expected end state on shutdown: target-server stats block prints `violation_total : 0`.

## 6. Validate User Story 2 — backoff blocks the entity queue (SC-002, SC-003)

Restart the target server with fault injection on:

```bash
go run ./06_target_server --fault-429-percent 30 --fault-5xx-percent 0
```

Restart the producer + worker as in steps 3–4. Run for ~60 seconds.

Verify:

- Worker slog shows `MarkFailedWithBackoff` calls (look for `"task processor failed"` followed by
  the same `task_id` re-acquired several seconds later — confirming the head-of-entity stays at
  the same `entity_seq` across retries).
- Target-server `violation_total` remains `0` despite ~30 % fault rate (SC-009).
- `curl -s localhost:9091/metrics | grep target_server_fault_injected_total` shows the `429`
  counter rising; the observed rate is within ±2 pp of 30 % over ≥ 10 000 requests (SC-008).

## 7. Validate User Story 3 — terminal failure (SC-004, SC-005)

Restart the target server with `--fault-429-percent 100` (every request fails) and the worker
with `--max-attempts 3`.

Produce 5 tasks for one specific entity (run the producer briefly with `--entities 1
--rate 5 --duration 1s`). Within ~30 seconds:

- Worker slog shows the head task transitioning to `status=failed` after 3 backoffs.
- Direct YDB query:

  ```sql
  SELECT id, entity_seq, status
  FROM ordered_tasks
  WHERE entity_id = 'entity-0000000'
  ORDER BY entity_seq;
  ```

  — exactly one row in `failed`, the others still in `pending`, none in `locked`.

- Operator resolution:

  ```sql
  UPDATE ordered_tasks
  SET status='skipped', resolved_by='operator-quickstart', resolved_at=CurrentUtcTimestamp()
  WHERE entity_id='entity-0000000' AND status='failed';
  ```

  After this, restart the target server with fault injection off — the next dispatch cycle
  (≤ 5 seconds) picks up the next `entity_seq` for that entity (SC-005).

## 8. Shutdown verification

Send `SIGTERM` (or `Ctrl-C`) to each process. Each prints its plain stats block to stdout.
Confirm:

- Producer: `total_inserted` ≈ rate × runtime within 5 %.
- Worker: `processed` count equals target server's `accepted_total` minus duplicates.
- Target server: `violation_total : 0` for runs without fault injection (SC-007); exact
  fault-rate counts within ±2 pp of configured (SC-008).

## Build gate (constitution requirement)

Before merging the feature branch:

```bash
go vet ./...
go build -o /dev/null ./05_ordered_tasks/cmd/producer
go build -o /dev/null ./05_ordered_tasks/cmd/worker
go build -o /dev/null ./06_target_server
```

All three must succeed with no warnings.
