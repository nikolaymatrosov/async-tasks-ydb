package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ydb-platform/ydb-go-sdk/v3/coordination"

	"async-tasks-ydb/04_coordinated_table/pkg/metrics"
	"async-tasks-ydb/04_coordinated_table/pkg/rebalancer"
	"async-tasks-ydb/04_coordinated_table/pkg/taskworker"
	"async-tasks-ydb/04_coordinated_table/pkg/uid"
	"async-tasks-ydb/04_coordinated_table/pkg/ydbconn"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	endpointFlag := flag.String("endpoint", os.Getenv("YDB_ENDPOINT"), "YDB gRPC endpoint")
	databaseFlag := flag.String("database", os.Getenv("YDB_DATABASE"), "YDB database path")
	partitionsFlag := flag.Int("partitions", 256, "number of logical partitions")
	coordinationPathFlag := flag.String("coordination-path", "", "coordination node path (default: <database>/04_coordinated_table)")
	lockDurationFlag := flag.Duration("lock-duration", 5*time.Second, "lock expiry duration per task")
	backoffMinFlag := flag.Duration("backoff-min", 50*time.Millisecond, "initial backoff on empty poll")
	backoffMaxFlag := flag.Duration("backoff-max", 5*time.Second, "maximum backoff on empty poll")
	metricsPortFlag := flag.Int("metrics-port", 9090, "port for Prometheus /metrics endpoint")
	flag.Parse()

	if *endpointFlag == "" {
		slog.Error("--endpoint or YDB_ENDPOINT is required")
		os.Exit(1)
	}
	if *databaseFlag == "" {
		slog.Error("--database is required")
		os.Exit(1)
	}

	coordinationPath := *coordinationPathFlag
	if coordinationPath == "" {
		coordinationPath = *databaseFlag + "/04_coordinated_table"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	db, err := ydbconn.Open(ctx, *endpointFlag, *databaseFlag)
	if err != nil {
		slog.Error("ydb.Open failed", "err", err)
		os.Exit(1)
	}
	defer db.Close(context.Background()) //nolint:errcheck

	err = db.Coordination().CreateNode(ctx, coordinationPath, coordination.NodeConfig{
		SelfCheckPeriodMillis:    1000,
		SessionGracePeriodMillis: 10000,
		ReadConsistencyMode:      coordination.ConsistencyModeStrict,
		AttachConsistencyMode:    coordination.ConsistencyModeStrict,
		RatelimiterCountersMode:  coordination.RatelimiterCountersModeDetailed,
	})
	if err != nil {
		slog.Debug("coordination node create returned error (may already exist)", "err", err)
	}
	slog.Info("coordination node ready", "path", coordinationPath)

	workerID := newUUID()

	stats := metrics.NewStats(workerID)

	addr := fmt.Sprintf(":%d", *metricsPortFlag)
	go http.ListenAndServe(addr, metrics.Handler(stats.Registry)) //nolint:errcheck
	slog.Info("metrics server started", "addr", addr)

	slog.Info("worker starting", "worker_id", workerID)
	go stats.Display(ctx)

	rb := rebalancer.NewRebalancer(db, coordinationPath, workerID, *partitionsFlag)
	partitionCh, err := rb.Start(ctx)
	if err != nil {
		slog.Error("rebalancer start failed", "err", err)
		return
	}

	worker := &taskworker.Worker{
		DB:           db,
		WorkerID:     workerID,
		LockDuration: *lockDurationFlag,
		BackoffMin:   *backoffMinFlag,
		BackoffMax:   *backoffMaxFlag,
		Stats:        stats,
	}
	worker.Run(ctx, partitionCh)

	rb.Stop()
	slog.Info("worker shutdown complete", "worker_id", workerID)
}

func newUUID() string {
	id, err := uid.GenerateUUID()
	if err != nil {
		panic(fmt.Sprintf("uuid generation failed: %v", err))
	}
	return id
}
