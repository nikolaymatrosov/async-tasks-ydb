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

	"async-tasks-ydb/04_coordinated_table/pkg/metrics"
	"async-tasks-ydb/04_coordinated_table/pkg/taskproducer"
	"async-tasks-ydb/04_coordinated_table/pkg/ydbconn"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	endpointFlag := flag.String("endpoint", os.Getenv("YDB_ENDPOINT"), "YDB gRPC endpoint")
	databaseFlag := flag.String("database", os.Getenv("YDB_DATABASE"), "YDB database path")
	partitionsFlag := flag.Int("partitions", 256, "number of logical partitions")
	coordinationPathFlag := flag.String("coordination-path", "", "coordination node path (unused by producer; kept for parity)")
	rateFlag := flag.Int("rate", 100, "target tasks per second")
	batchWindowFlag := flag.Duration("batch-window", 100*time.Millisecond, "batch accumulation window")
	reportIntervalFlag := flag.Duration("report-interval", 5*time.Second, "throughput reporting interval")
	metricsPortFlag := flag.Int("metrics-port", 9090, "port for Prometheus /metrics endpoint")
	flag.Parse()

	_ = coordinationPathFlag

	if *endpointFlag == "" {
		slog.Error("--endpoint or YDB_ENDPOINT is required")
		os.Exit(1)
	}
	if *databaseFlag == "" {
		slog.Error("--database is required")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	db, err := ydbconn.Open(ctx, *endpointFlag, *databaseFlag)
	if err != nil {
		slog.Error("ydb.Open failed", "err", err)
		os.Exit(1)
	}
	defer db.Close(context.Background()) //nolint:errcheck

	ps := metrics.NewProducerStats(float64(*rateFlag), *batchWindowFlag)
	ps.Up.Set(1)
	defer ps.Up.Set(0)

	addr := fmt.Sprintf(":%d", *metricsPortFlag)
	go http.ListenAndServe(addr, metrics.Handler(ps.Registry)) //nolint:errcheck
	slog.Info("metrics server started", "addr", addr)

	taskproducer.Produce(ctx, db, *rateFlag, *partitionsFlag, *batchWindowFlag, *reportIntervalFlag, ps)
}
