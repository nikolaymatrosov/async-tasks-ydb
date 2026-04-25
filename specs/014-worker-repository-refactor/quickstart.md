# Quickstart: Verifying the Worker Repository Refactor

**Branch**: `014-worker-repository-refactor` | **Date**: 2026-04-25

This quickstart is for reviewers and operators who need to confirm the refactor preserves behaviour. It exercises three checks: a static-inspection check (SC-001, SC-002, SC-005), a unit-test check (SC-004), and a live-run regression check (SC-003).

## Prerequisites

- Go 1.26 toolchain (`go version` ≥ go1.26).
- For the live-run check: a YDB endpoint with the existing `coordinated_tasks` table populated by the producer (no schema changes needed).

## 1. Static-inspection check (no runtime needed)

Verify that the worker file is free of SQL, parameter builders, and transaction settings — the central claim of the refactor.

```sh
cd /Users/nikthespirit/Documents/experiment/async-tasks-ydb

# SC-001: zero SQL keywords, zero ParamsBuilder, zero TxControl in worker.go
grep -nE 'SELECT|UPDATE|UPSERT|DECLARE|ParamsBuilder|WithTxControl|WithTxSettings|SerializableReadWrite|SnapshotReadOnly' \
    04_coordinated_table/pkg/taskworker/worker.go
# Expected: no matches
```

```sh
# SC-005: every reference to coordinated_tasks column names lives in the repository file
grep -nE 'coordinated_tasks|locked_until|lock_value|partition_id|status\s*=\s*.pending|scheduled_at' \
    04_coordinated_table/pkg/taskworker/*.go | \
    grep -v 'repository_ydb.go'
# Expected: no matches outside repository_ydb.go
```

```sh
# SC-002: orchestration-loop function shorter than the pre-refactor lockNextTask+completeTask
#   pre-refactor lines (worker.go before this branch):
#     lockNextTask: ~159 LOC (worker.go:173–331)
#     completeTask: ~50  LOC (worker.go:333–382)
#     total: ~209 LOC of orchestration+SQL
#   post-refactor target: orchestration alone ≤ 145 LOC (≥30% reduction)
awk '/^func \(w \*Worker\) processPartition/,/^}/' 04_coordinated_table/pkg/taskworker/worker.go | \
    grep -cvE '^\s*$|^\s*//'
# Expected: a number ≪ 209
```

If any of these checks fail, the refactor is incomplete.

## 2. Unit-test check (no database needed)

Run the new orchestration test suite. This is the SC-004 verification — it must pass with no `YDB_ENDPOINT` set.

```sh
unset YDB_ENDPOINT YDB_DATABASE
go test ./04_coordinated_table/pkg/taskworker/...
```

Expected output (indicative):

```text
ok      async-tasks-ydb/04_coordinated_table/pkg/taskworker    0.0XXs
```

The test cases covered (defined in `worker_test.go`) MUST include:

| Case | Setup (fake repository) | Assertion |
| ---- | ----------------------- | --------- |
| Backoff escalates on empty polls | `FetchEligibleCandidate` returns `(nil, nil)` for several iterations | Sleep durations grow `BackoffMin → 2×BackoffMin → … → BackoffMax`; `ProcessTask` never called. |
| Lost-race no-op | `FetchEligibleCandidate` returns a candidate; `ClaimTask` returns `(nil, nil)` | `ProcessTask` never called; `MarkCompleted` never called; `Stats.Errors` unchanged. |
| Successful claim → process → complete | `FetchEligibleCandidate` returns candidate; `ClaimTask` returns `ClaimedTask`; `ProcessTask` returns `nil` | `MarkCompleted` called exactly once with the same `ID` and `LockValue`; `Stats.Processed` incremented. |
| Processor error skips completion | `ClaimTask` succeeds; `ProcessTask` returns non-nil | `MarkCompleted` NOT called; `Stats.Errors` incremented; backoff NOT reset. |
| Transient repository error | `FetchEligibleCandidate` returns `(nil, transientErr)` | `Stats.Errors` incremented; backoff escalates; iteration continues. |
| Lease cancellation | `leaseCtx` cancelled mid-iteration | Worker exits the partition loop without recording an error. |

## 3. Live-run regression check (requires YDB)

Confirm the throughput / error-rate / lock-claim-rate are within ±5 % of the pre-refactor baseline (SC-003).

### Baseline (capture once, before merging)

On the **pre-refactor** commit (`git checkout 014-worker-repository-refactor~1` or main), run:

```sh
export YDB_ENDPOINT=...    # your dev/staging endpoint
export YDB_DATABASE=/...   # your database path

# Terminal A: producer at fixed rate
go run ./04_coordinated_table/cmd/producer/ \
    -rate 500 -partitions 256

# Terminal B: worker with metrics on :9090
go run ./04_coordinated_table/cmd/worker/ \
    -partitions 256 -lock-duration 5s

# After 10 minutes of steady state, snapshot the metrics:
curl -s localhost:9090/metrics | \
    grep -E 'tasks_locked_total|tasks_processed_total|tasks_errors_total' \
    > /tmp/baseline.txt
```

### Post-refactor measurement

Switch to the refactored commit (`git checkout 014-worker-repository-refactor`) and repeat the same workload for the same duration, snapshotting to `/tmp/refactor.txt`.

### Compare

For each counter (`tasks_locked_total`, `tasks_processed_total`, `tasks_errors_total`), compute the per-second rate and verify it is within ±5 % of baseline. Lock-claim correctness is implicit: if `tasks_processed_total / tasks_locked_total ≈ 1.0` in both runs, the conditional-claim semantics are unchanged.

Expected `slog` output in the worker terminal (post-refactor) MUST match the pre-refactor output line-for-line in fields and levels, e.g.:

```json
{"time":"...","level":"INFO","msg":"task locked","worker_id":"...","partition_id":42,"task_id":"...","priority":137}
{"time":"...","level":"INFO","msg":"task completed","worker_id":"...","partition_id":42,"task_id":"..."}
```

If any field is missing or renamed, FR-008 is violated.

## 4. Build gate (before merge)

```sh
go build -o /dev/null ./04_coordinated_table/cmd/worker/
go build -o /dev/null ./04_coordinated_table/cmd/producer/
go vet ./04_coordinated_table/...
```

All three MUST exit 0. (Per `MEMORY.md`, do not run `go build ./04_coordinated_table/...` from inside the package — use `-o /dev/null` or `go vet`.)

## What success looks like

A reviewer who has run all four checks should be able to say:

- `worker.go` reads as a clean orchestration loop — no SQL.
- `repository.go` + `repository_ydb.go` express the storage boundary in domain terms.
- `worker_test.go` exercises every orchestration path without a database.
- Live throughput, error rate, and `slog` output match the pre-refactor baseline.

If all four hold, the refactor satisfies its specification.
