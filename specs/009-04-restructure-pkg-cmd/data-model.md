# Data Model: 009-04-restructure-pkg-cmd

This is a structural refactor — no YDB schema changes. The data model documents the Go package boundaries and type ownership.

## Package Boundary Map

```
async-tasks-ydb/04_coordinated_table/
│
├── pkg/uid/
│   └── uid.go
│       ├── func generateUUID() (string, error)    ← used by taskproducer (buildBatch) + cmd/worker (workerID init)
│
├── pkg/metrics/
│   └── metrics.go
│       ├── func metricsHandler(registry *prometheus.Registry) http.Handler
│   └── worker_stats.go
│       ├── type Stats struct { workerID, startTime, registry, processed, locked, errors, partitions, up }
│       ├── func newStats(workerID string) *Stats
│       ├── func readCounter(c prometheus.Counter) int64
│       ├── func readGauge(g prometheus.Gauge) int64
│       └── func (s *Stats) display(ctx context.Context)
│   └── producer_stats.go
│       ├── type ProducerStats struct { registry, startTime, up, targetRate, windowSeconds, ... }
│       └── func newProducerStats(targetRate float64, window time.Duration) *ProducerStats
│
├── pkg/rebalancer/
│   └── rebalancer.go
│       ├── type partitionEvent struct { partitionID int; lease coordination.Lease }
│       ├── type Rebalancer struct { ... }
│       ├── func newRebalancer(db *ydb.Driver, coordinationPath, workerID string, partitionCount int) *Rebalancer
│       ├── func (r *Rebalancer) start(ctx) (<-chan partitionEvent, error)
│       └── func (r *Rebalancer) stop()
│
├── pkg/taskworker/
│   └── worker.go
│       ├── type lockedTask struct { id, partitionID, priority, lockValue }
│       ├── type Worker struct { db, workerID, lockDuration, backoffMin, backoffMax, stats }
│       └── func (w *Worker) run(ctx, partitionCh)
│
├── pkg/taskproducer/
│   └── producer.go
│       ├── type taskRow struct { id, hash, partitionID, priority, payload, createdAt, scheduledAt }
│       ├── func buildBatch(ctx, batchSize, partitions) []taskRow
│       ├── func upsertBatch(ctx, db, batch) error
│       └── func Produce(ctx, db, rate, partitions, batchWindow, reportInterval, ps)
│
├── pkg/ydbconn/
│   └── conn.go
│       └── func Open(ctx, endpoint string) (*ydb.Driver, error)
│              resolves credentials from: YDB_SA_KEY_FILE → yc.WithServiceAccountKeyFileCredentials
│                                         YDB_ANONYMOUS_CREDENTIALS=1 → ydb.WithAnonymousCredentials
│                                         (default) → yc.WithMetadataCredentials
│
├── cmd/producer/
│   └── main.go
│       Flags: --endpoint, --database, --partitions, --coordination-path,
│              --rate, --batch-window, --report-interval, --metrics-port
│       Calls: ydbconn.Open → metrics server → taskproducer.Produce
│
└── cmd/worker/
    └── main.go
        Flags: --endpoint, --database, --partitions, --coordination-path,
               --lock-duration, --backoff-min, --backoff-max, --metrics-port
        Calls: ydbconn.Open → coordination.CreateNode → metrics server → rebalancer.start → Worker.run
```

## Type Ownership & Visibility

| Type | Package | Exported? | Used by |
|---|---|---|---|
| `Stats` | `pkg/metrics` | Yes | `cmd/worker/main.go`, `pkg/taskworker` |
| `ProducerStats` | `pkg/metrics` | Yes | `cmd/producer/main.go`, `pkg/taskproducer` |
| `Rebalancer` | `pkg/rebalancer` | Yes | `cmd/worker/main.go` |
| `partitionEvent` | `pkg/rebalancer` | Yes | `pkg/taskworker` (via channel receive) |
| `Worker` | `pkg/taskworker` | Yes | `cmd/worker/main.go` |
| `lockedTask` | `pkg/taskworker` | No (internal) | `pkg/taskworker` only |
| `taskRow` | `pkg/taskproducer` | No (internal) | `pkg/taskproducer` only |

## Dependency Graph (no cycles)

```
cmd/producer → pkg/ydbconn, pkg/metrics, pkg/taskproducer, pkg/uid (indirect via taskproducer)
cmd/worker   → pkg/ydbconn, pkg/metrics, pkg/taskworker, pkg/rebalancer, pkg/uid

pkg/taskproducer → pkg/metrics, pkg/uid
pkg/taskworker   → pkg/metrics, pkg/rebalancer (partitionEvent type)
pkg/rebalancer   → (no pkg/ deps — only ydb-go-sdk)
pkg/metrics      → (no pkg/ deps — only prometheus)
pkg/ydbconn      → (no pkg/ deps — only ydb-go-sdk + ydb-go-yc)
pkg/uid          → (no pkg/ deps — only uuid)
```
