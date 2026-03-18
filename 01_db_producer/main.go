package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/google/uuid"
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	yc "github.com/ydb-platform/ydb-go-yc"
)

func generatePayload(size int) []byte {
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		log.Fatalf("rand.Read: %v", err)
	}
	return payload
}

func main() {
	payloadSize  := flag.Int("payload-size", 1024, "Size of payload per row in bytes")
	parallelism  := flag.Int("parallelism", 10, "Number of concurrent worker goroutines")
	flag.Parse()

	endpoint := os.Getenv("YDB_ENDPOINT")
	if endpoint == "" {
		log.Fatal("YDB_ENDPOINT is not set")
	}
	var creds ydb.Option
	if saKeyFile := os.Getenv("YDB_SA_KEY_FILE"); saKeyFile != "" {
		creds = yc.WithServiceAccountKeyFileCredentials(saKeyFile)
	} else {
		creds = yc.WithMetadataCredentials()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := ydb.Open(ctx, endpoint,
		creds,
		yc.WithInternalCA(),
	)
	if err != nil {
		log.Fatalf("ydb.Open: %v", err)
	}
	defer db.Close(context.Background())

	var (
		totalRows     atomic.Int64
		totalBytes    atomic.Int64
		totalDuration atomic.Int64 // nanoseconds
	)
	startTime := time.Now()
	payloadLen := int64(*payloadSize)

	pool := pond.NewPool(*parallelism, pond.WithContext(ctx), pond.WithQueueSize(*parallelism))

	log.Printf("Producer started: payload=%d bytes, workers=%d. Press Ctrl-C to stop.", *payloadSize, *parallelism)

	for {
		err := pool.Go(func() {
			id := uuid.New()
			now := time.Now()
			payload := generatePayload(*payloadSize)
			t0 := time.Now()
			if err := db.Query().Exec(ctx,
				`UPSERT INTO tasks (id, payload, created_at) VALUES ($id, $payload, $created_at)`,
				query.WithParameters(
					ydb.ParamsBuilder().
						Param("$id").Uuid(id).
						Param("$payload").Bytes(payload).
						Param("$created_at").Timestamp(now).
						Build(),
				),
			); err != nil {
				return
			}
			totalDuration.Add(time.Since(t0).Nanoseconds())
			totalRows.Add(1)
			totalBytes.Add(payloadLen)
		})
		if err != nil {
			// Pool stopped because context was cancelled (Ctrl-C / SIGTERM)
			break
		}
	}

	pool.StopAndWait()

	elapsed := time.Since(startTime).Seconds()
	rows := totalRows.Load()
	bytes := totalBytes.Load()

	totalNs := totalDuration.Load()

	var rowsPerSec, mbps, avgMs float64
	if elapsed > 0 {
		rowsPerSec = float64(rows) / elapsed
		mbps = float64(bytes) * 8 / elapsed / 1_000_000
	}
	if rows > 0 {
		avgMs = float64(totalNs) / float64(rows) / 1e6
	}

	fmt.Printf("\n--- Stats ---\n")
	fmt.Printf("Rows inserted : %d\n", rows)
	fmt.Printf("Elapsed       : %.2f s\n", elapsed)
	fmt.Printf("Rows/sec      : %.2f\n", rowsPerSec)
	fmt.Printf("Throughput    : %.4f Mbps\n", mbps)
	fmt.Printf("Avg insert    : %.2f ms\n", avgMs)
}
