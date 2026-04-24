# Implementation Plan: Worker Task Processor — API Gateway Call

**Branch**: `010-yc-apigw-dummy` | **Date**: 2026-04-24 | **Spec**: [spec.md](spec.md)

## Summary

Add real work to the coordinated-table worker: the producer serialises the API Gateway URL into each task's JSON payload; the worker unmarshals it, makes one HTTP GET, and records the response status in a new labelled Prometheus counter before marking the task `completed`. No schema changes required.

## Technical Context

**Language/Version**: Go 1.26 (go.mod)
**Primary Dependencies**: `ydb-go-sdk/v3 v3.127.0`, `ydb-go-yc v0.12.3`, `murmur3 v1.1.8`, `uuid v1.6.0`, `prometheus/client_golang` (already in go.mod via 006-prometheus-metrics), stdlib `net/http`, `encoding/json` — **no new direct dependencies**
**Storage**: YDB `coordinated_tasks` table — existing `payload Utf8 NOT NULL` column, no DDL changes
**Testing**: Manual end-to-end against live YDB + live API Gateway per constitution
**Target Platform**: Linux server (Cloud Run container, same as current workers)
**Project Type**: CLI binary / worker process
**Performance Goals**: No change from current baseline; HTTP call latency is the observable under study
**Constraints**: No new `go.mod` direct dependencies; context cancellation must propagate to in-flight HTTP calls
**Scale/Scope**: Single API Gateway endpoint, same worker concurrency as 04 example

## Constitution Check

| Principle | Check |
| --- | --- |
| I. Self-Contained Examples — single `main.go`, own top-level dir | ⚠️ pre-existing justified deviation (pkg/cmd layout introduced in 009) |
| II. Lifecycle Completeness — `signal.NotifyContext`, clean shutdown | ✅ already in both `cmd/producer/main.go` and `cmd/worker/main.go` |
| III. Schema-Managed Persistence — all DDL in `migrations/` | ✅ no schema changes; existing migration unchanged |
| IV. Environment-Variable Configuration — no hardcoded creds/endpoints | ✅ `APIGW_URL` env var with `--apigw-url` flag; missing = fail fast |
| V. Structured Logging — `log/slog` JSON handler, structured fields | ✅ existing handler; new log fields follow established pattern |
| Tech Constraints — Go 1.26, YDB SDK, murmur3, goose | ✅ no new tools or languages |

## Project Structure

### Documentation (this feature)

```text
specs/010-yc-apigw-dummy/
├── plan.md          ← this file
├── research.md      ← Phase 0
├── data-model.md    ← Phase 1
└── tasks.md         ← /speckit-tasks (not yet created)
```

### Source Code (changed files only)

```text
04_coordinated_table/
├── cmd/
│   ├── producer/
│   │   └── main.go          ← add --apigw-url flag; pass URL to Produce()
│   └── worker/
│       └── main.go          ← define newAPIGWProcessor(); wire into Worker
└── pkg/
    ├── metrics/
    │   └── worker_stats.go  ← add APIGWCalls *prometheus.CounterVec
    ├── taskproducer/
    │   └── producer.go      ← serialize JSON payload {"url":"<apigwURL>"}
    └── taskworker/
        └── worker.go        ← add payload to lockedTask & SELECT; add ProcessTask field
```

## Complexity Tracking

| Violation                 | Why Needed                                              | Simpler Alternative Rejected Because                            |
| ------------------------- | ------------------------------------------------------- | --------------------------------------------------------------- |
| Principle I: pkg/cmd layout | Introduced in 009 to manage growing complexity of 04 example | Merging back to single main.go would discard the restructuring already merged |

---

## Phase 1 Design Details

### 1. `pkg/taskproducer/producer.go`

**Change**: `buildBatch` and `Produce` receive `apigwURL string`.

```go
// in buildBatch:
payload := fmt.Sprintf(`{"url":%q}`, apigwURL)
```

`Produce` signature becomes:

```go
func Produce(ctx context.Context, db *ydb.Driver, rate int, partitions int,
    batchWindow time.Duration, reportInterval time.Duration,
    ps *metrics.ProducerStats, apigwURL string)
```

### 2. `cmd/producer/main.go`

Add flag:

```go
apigwURLFlag := flag.String("apigw-url", os.Getenv("APIGW_URL"), "API Gateway base URL for task payloads")
```

Validate non-empty (fail fast with `slog.Error` + `os.Exit(1)`), then pass to `taskproducer.Produce`.

### 3. `pkg/taskworker/worker.go`

**`lockedTask`** — add field:

```go
payload string
```

**`Worker`** — add field:

```go
ProcessTask func(ctx context.Context, taskID string, payload string) error
```

**`lockNextTask`** — extend SELECT to include `payload`:

```sql
SELECT id, priority, payload FROM coordinated_tasks WHERE …
```

Scan `payload` into `lockedTask.payload`.

**`completeTask`** — replace `time.Sleep(100ms)` with:

```go
if w.ProcessTask != nil {
    if err := w.ProcessTask(context.Background(), task.id, task.payload); err != nil {
        w.Stats.Errors.Add(1)
        slog.Warn("task processor failed", "worker_id", w.WorkerID, "task_id", task.id, "err", err)
        return   // leave task locked; lock will expire and be retried
    }
}
```

The DB `UPDATE … SET status='completed'` block that follows is unchanged.

### 4. `pkg/metrics/worker_stats.go`

Add to `Stats`:

```go
APIGWCalls *prometheus.CounterVec
```

Create in `NewStats`:

```go
apigwCalls := prometheus.NewCounterVec(prometheus.CounterOpts{
    Name:        "coordinator_apigw_calls_total",
    Help:        "HTTP calls made to the API Gateway, by response status",
    ConstLabels: prometheus.Labels{"worker_id": workerID},
}, []string{"http_status"})
registry.MustRegister(apigwCalls)
```

Return field in struct literal:

```go
APIGWCalls: apigwCalls,
```

### 5. `cmd/worker/main.go`

Define the processor closure and wire it:

```go
func newAPIGWProcessor(stats *metrics.Stats) func(ctx context.Context, taskID, payload string) error {
    return func(ctx context.Context, taskID, payload string) error {
        var p struct {
            URL string `json:"url"`
        }
        if err := json.Unmarshal([]byte(payload), &p); err != nil || p.URL == "" {
            stats.APIGWCalls.WithLabelValues("error").Inc()
            return fmt.Errorf("invalid payload: %w", err)
        }
        req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, strings.NewReader(payload))
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("X-Task-ID", taskID)
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            stats.APIGWCalls.WithLabelValues("error").Inc()
            return err
        }
        defer resp.Body.Close()
        status := strconv.Itoa(resp.StatusCode)
        stats.APIGWCalls.WithLabelValues(status).Inc()
        slog.Info("apigw call",
            "task_id", taskID,
            "url", p.URL,
            "http_status", resp.StatusCode,
        )
        if resp.StatusCode != http.StatusOK {
            return fmt.Errorf("apigw returned %d", resp.StatusCode)
        }
        return nil
    }
}
```

Set on worker before `Run`:

```go
worker.ProcessTask = newAPIGWProcessor(stats)
```

---

## Acceptance Baseline (manual validation)

After running producer + worker against a live YDB instance with a real API Gateway:

1. `coordinator_apigw_calls_total{http_status="200"}` increments — visible on `/metrics`.
2. `slog` output contains `"apigw call"` entries with `task_id` and `http_status=200`.
3. `coordinated_tasks` rows transition to `status='completed'`.
4. On a forced non-200 response: `coordinator_apigw_calls_total{http_status="503"}` appears and the task remains `locked` until its lock expires.
