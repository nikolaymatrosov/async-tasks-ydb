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

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/coordination"
	yc "github.com/ydb-platform/ydb-go-yc"
)

func main() {
	// Configure structured JSON logging.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	endpointFlag := flag.String("endpoint", os.Getenv("YDB_ENDPOINT"), "YDB gRPC endpoint")
	databaseFlag := flag.String("database", os.Getenv("YDB_DATABASE"), "YDB database path")
	modeFlag := flag.String("mode", "", "producer or worker")
	partitionsFlag := flag.Int("partitions", 256, "number of logical partitions")
	coordinationPathFlag := flag.String("coordination-path", "", "coordination node path (default: <database>/04_coordinated_table)")
	rateFlag := flag.Int("rate", 100, "producer: tasks per second")
	batchWindowFlag := flag.Duration("batch-window", 100*time.Millisecond, "producer: batch window duration")
	reportIntervalFlag := flag.Duration("report-interval", 5*time.Second, "producer: throughput report interval")
	lockDurationFlag := flag.Duration("lock-duration", 5*time.Second, "worker: lock expiry duration")
	backoffMinFlag := flag.Duration("backoff-min", 50*time.Millisecond, "worker: initial backoff on empty poll")
	backoffMaxFlag := flag.Duration("backoff-max", 5*time.Second, "worker: max backoff on empty poll")
	metricsPortFlag := flag.Int("metrics-port", 9090, "port for Prometheus /metrics endpoint")
	flag.Parse()

	// Validate required flags.
	if *endpointFlag == "" {
		slog.Error("--endpoint or YDB_ENDPOINT is required")
		os.Exit(1)
	}
	if *databaseFlag == "" {
		slog.Error("--database is required")
		os.Exit(1)
	}
	if *modeFlag != "producer" && *modeFlag != "worker" {
		slog.Error("--mode must be 'producer' or 'worker'", "got", *modeFlag)
		os.Exit(1)
	}

	coordinationPath := *coordinationPathFlag
	if coordinationPath == "" {
		coordinationPath = *databaseFlag + "/04_coordinated_table"
	}

	// Determine credentials.
	var creds ydb.Option
	if saKeyFile := os.Getenv("YDB_SA_KEY_FILE"); saKeyFile != "" {
		creds = yc.WithServiceAccountKeyFileCredentials(saKeyFile)
	} else if os.Getenv("YDB_ANONYMOUS_CREDENTIALS") == "1" {
		creds = ydb.WithAnonymousCredentials()
	} else {
		creds = yc.WithMetadataCredentials()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Open YDB driver.
	db, err := ydb.Open(ctx, *endpointFlag,
		creds,
		yc.WithInternalCA(),
	)
	if err != nil {
		slog.Error("ydb.Open failed", "err", err)
		os.Exit(1)
	}
	defer db.Close(context.Background()) //nolint:errcheck

	// Create coordination node (idempotent).
	err = db.Coordination().CreateNode(ctx, coordinationPath, coordination.NodeConfig{
		SelfCheckPeriodMillis:    1000,
		SessionGracePeriodMillis: 10000,
		ReadConsistencyMode:      coordination.ConsistencyModeStrict,
		AttachConsistencyMode:    coordination.ConsistencyModeStrict,
		RatelimiterCountersMode:  coordination.RatelimiterCountersModeDetailed,
	})
	if err != nil {
		// Ignore "already exists" — CreateNode is idempotent in YDB SDK.
		slog.Debug("coordination node create returned error (may already exist)", "err", err)
	}
	slog.Info("coordination node ready", "path", coordinationPath)

	workerID := newUUID()
	addr := fmt.Sprintf(":%d", *metricsPortFlag)

	switch *modeFlag {
	case "producer":
		ps := newProducerStats(float64(*rateFlag), *batchWindowFlag)
		ps.up.Set(1)
		defer ps.up.Set(0)
		go http.ListenAndServe(addr, metricsHandler(ps.registry)) //nolint:errcheck
		slog.Info("metrics server started", "addr", addr)
		runProducer(ctx, db, *rateFlag, *partitionsFlag, *batchWindowFlag, *reportIntervalFlag, ps)
	case "worker":
		stats := newStats(workerID)
		go http.ListenAndServe(addr, metricsHandler(stats.registry)) //nolint:errcheck
		slog.Info("metrics server started", "addr", addr)
		runWorker(ctx, db, coordinationPath, *partitionsFlag, *lockDurationFlag, *backoffMinFlag, *backoffMaxFlag, workerID, stats)
	}

	// Ensure clean exit after context cancellation.
	if ctx.Err() != nil {
		slog.Info("shutdown complete")
	}
}

// runWorker is the entry point for worker mode — declared here, implemented in worker.go.
func runWorker(
	ctx context.Context,
	db *ydb.Driver,
	coordinationPath string,
	partitions int,
	lockDuration time.Duration,
	backoffMin time.Duration,
	backoffMax time.Duration,
	workerID string,
	stats *Stats,
) {
	slog.Info("worker starting", "worker_id", workerID)
	go stats.display(ctx)

	rebalancer := newRebalancer(db, coordinationPath, workerID, partitions)
	partitionCh, err := rebalancer.start(ctx)
	if err != nil {
		slog.Error("rebalancer start failed", "err", err)
		return
	}

	worker := &Worker{
		db:           db,
		workerID:     workerID,
		lockDuration: lockDuration,
		backoffMin:   backoffMin,
		backoffMax:   backoffMax,
		stats:        stats,
	}
	worker.run(ctx, partitionCh)

	rebalancer.stop()
	slog.Info("worker shutdown complete", "worker_id", workerID)
}

// runProducer is the entry point for producer mode — declared here, implemented in producer.go.
func runProducer(ctx context.Context, db *ydb.Driver, rate int, partitions int, batchWindow time.Duration, reportInterval time.Duration, ps *ProducerStats) {
	produce(ctx, db, rate, partitions, batchWindow, reportInterval, ps)
}

// newUUID generates a new UUID string.
func newUUID() string {
	id, err := generateUUID()
	if err != nil {
		// uuid generation should not fail under normal circumstances
		panic(fmt.Sprintf("uuid generation failed: %v", err))
	}
	return id
}
