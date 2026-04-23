package taskproducer

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

	"async-tasks-ydb/04_coordinated_table/pkg/metrics"
	"async-tasks-ydb/04_coordinated_table/pkg/uid"
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

		taskID, err := uid.GenerateUUID()
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

// Produce runs the fixed-window batch loop until ctx is cancelled.
func Produce(ctx context.Context, db *ydb.Driver, rate int, partitions int, batchWindow time.Duration, reportInterval time.Duration, ps *metrics.ProducerStats) {
	if rate <= 0 {
		rate = 1
	}

	effectiveWindow := batchWindow
	targetBatchSize := int(math.Round(float64(rate) * batchWindow.Seconds()))
	if targetBatchSize < 1 {
		targetBatchSize = 1
		effectiveWindow = time.Duration(float64(time.Second) / float64(rate))
	}

	ps.TargetBatchSize.Set(float64(targetBatchSize))
	ps.TargetRate.Set(float64(rate))
	ps.WindowSeconds.Set(batchWindow.Seconds())

	slog.Info("producer started", "rate", rate, "partitions", partitions, "batch_window", batchWindow, "target_batch_size", targetBatchSize)

	var insertedTotal atomic.Int64

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
				ps.ObservedRate.Set(rateObserved)
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

loop:
	for ctx.Err() == nil {
		windowStart := time.Now()
		batch := buildBatch(ctx, targetBatchSize, partitions)

		if err := upsertBatch(ctx, db, batch); err != nil {
			if ctx.Err() != nil {
				break loop
			}
			slog.Warn("upsert batch failed", "err", err)
			ps.BatchErrors.Inc()
			continue
		}

		n := int64(len(batch))
		insertedTotal.Add(n)
		ps.Inserted.Add(float64(n))
		ps.Batches.Inc()
		ps.BatchSize.Observe(float64(n))

		elapsed := time.Since(windowStart)
		ps.BatchDuration.Observe(elapsed.Seconds())

		if elapsed >= effectiveWindow {
			ps.Backpressure.Inc()
		} else {
			select {
			case <-ctx.Done():
				break loop
			case <-time.After(effectiveWindow - elapsed):
			}
		}
	}

	total := insertedTotal.Load()
	finalRate := float64(total) / time.Since(ps.StartTime).Seconds()
	slog.Info("producer stopping", "total_inserted", total, "rate_observed", finalRate)
}
