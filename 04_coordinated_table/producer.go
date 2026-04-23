package main

import (
	"context"
	"log/slog"
	"math"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/twmb/murmur3"
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/types"
)

type taskRow struct {
	id          string
	hash        int64
	partitionID uint16
	priority    uint8
	payload     string
	createdAt   time.Time
	scheduledAt *time.Time
}

func buildBatch(ctx context.Context, batchSize int, partitions int) []taskRow {
	rows := make([]taskRow, 0, batchSize)
	for range batchSize {
		select {
		case <-ctx.Done():
			return rows
		default:
		}

		taskID, err := generateUUID()
		if err != nil {
			continue
		}

		hash := int64(murmur3.Sum32([]byte(taskID)))
		partitionID := uint16(uint64(hash&0x7FFFFFFFFFFFFFFF) % uint64(partitions))
		priority := uint8(rand.Intn(256))
		payload := "task-payload-" + taskID
		now := time.Now().UTC()

		var scheduledAt *time.Time
		if rand.Intn(10) == 0 {
			t := now.Add(time.Duration(5+rand.Intn(26)) * time.Second)
			scheduledAt = &t
		}

		rows = append(rows, taskRow{
			id:          taskID,
			hash:        hash,
			partitionID: partitionID,
			priority:    priority,
			payload:     payload,
			createdAt:   now,
			scheduledAt: scheduledAt,
		})
	}
	return rows
}

func upsertBatch(ctx context.Context, db *ydb.Driver, batch []taskRow) error {
	records := make([]types.Value, 0, len(batch))
	for _, r := range batch {
		records = append(records, types.StructValue(
			types.StructFieldValue("id", types.TextValue(r.id)),
			types.StructFieldValue("hash", types.Int64Value(r.hash)),
			types.StructFieldValue("partition_id", types.Uint16Value(r.partitionID)),
			types.StructFieldValue("priority", types.Uint8Value(r.priority)),
			types.StructFieldValue("payload", types.TextValue(r.payload)),
			types.StructFieldValue("created_at", types.TimestampValueFromTime(r.createdAt)),
			types.StructFieldValue("scheduled_at", types.NullableTimestampValueFromTime(r.scheduledAt)),
		))
	}

	return db.Query().Exec(ctx,
		`UPSERT INTO coordinated_tasks
		SELECT id, hash, partition_id, priority, "pending" AS status, payload, created_at, scheduled_at
		FROM AS_TABLE($records)`,
		query.WithParameters(
			ydb.ParamsBuilder().
				Param("$records").Any(types.ListValue(records...)).
				Build(),
		),
	)
}

// produce runs the fixed-window batch loop until ctx is cancelled.
func produce(ctx context.Context, db *ydb.Driver, rate int, partitions int, batchWindow time.Duration, reportInterval time.Duration, ps *ProducerStats) {
	if rate <= 0 {
		rate = 1
	}

	// Low-rate edge case: when rate*window < 1 row, use a longer effective window.
	effectiveWindow := batchWindow
	targetBatchSize := int(math.Round(float64(rate) * batchWindow.Seconds()))
	if targetBatchSize < 1 {
		targetBatchSize = 1
		effectiveWindow = time.Duration(float64(time.Second) / float64(rate))
	}

	ps.targetBatchSize.Set(float64(targetBatchSize))
	ps.targetRate.Set(float64(rate))
	ps.windowSeconds.Set(batchWindow.Seconds())

	slog.Info("producer started", "rate", rate, "partitions", partitions, "batch_window", batchWindow, "target_batch_size", targetBatchSize)

	var insertedTotal atomic.Int64

	// Report goroutine (T009).
	go func() {
		ticker := time.NewTicker(reportInterval)
		defer ticker.Stop()
		var lastSnapshot int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snapshot := insertedTotal.Load()
				delta := snapshot - lastSnapshot
				lastSnapshot = snapshot
				rateObserved := float64(delta) / reportInterval.Seconds()
				ps.observedRate.Set(rateObserved)
				slog.Info("producer stats",
					"inserted_total", snapshot,
					"inserted_delta", delta,
					"rate_observed", rateObserved,
					"interval_s", reportInterval.Seconds(),
					"batch_window_ms", batchWindow.Milliseconds(),
				)
			}
		}
	}()

	// Batch loop.
loop:
	for ctx.Err() == nil {
		windowStart := time.Now()
		batch := buildBatch(ctx, targetBatchSize, partitions)

		if err := upsertBatch(ctx, db, batch); err != nil {
			if ctx.Err() != nil {
				break loop
			}
			slog.Warn("upsert batch failed", "err", err)
			ps.batchErrors.Inc()
			continue
		}

		n := int64(len(batch))
		insertedTotal.Add(n)
		ps.inserted.Add(float64(n))
		ps.batches.Inc()
		ps.batchSize.Observe(float64(n))

		elapsed := time.Since(windowStart)
		ps.batchDuration.Observe(elapsed.Seconds())

		// Backpressure: storage slower than window (T008).
		if elapsed >= effectiveWindow {
			ps.backpressure.Inc()
		} else {
			select {
			case <-ctx.Done():
				break loop
			case <-time.After(effectiveWindow - elapsed):
			}
		}
	}

	// Shutdown log (T010).
	total := insertedTotal.Load()
	finalRate := float64(total) / time.Since(ps.startTime).Seconds()
	slog.Info("producer stopping", "total_inserted", total, "rate_observed", finalRate)
}
