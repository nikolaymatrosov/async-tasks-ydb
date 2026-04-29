package taskproducer

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/twmb/murmur3"
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/types"

	"async-tasks-ydb/05_ordered_tasks/pkg/metrics"
	"async-tasks-ydb/05_ordered_tasks/pkg/uid"
)

// nextSeq generates a process-wide strictly-increasing 64-bit ordinal:
//
//	seq = UnixNano() * 1024 + atomic_tiebreaker
//
// monotonic across producer restarts (the nano clock keeps advancing), so the
// new run's seqs are strictly greater than any previously written seq for any
// entity — preserving the head-of-entity invariant under the single-instance
// producer assumption.
var seqCounter atomic.Uint64

func nextSeq() uint64 {
	return uint64(time.Now().UnixNano())*1024 + seqCounter.Add(1)
}

type taskRow struct {
	id          string
	partitionID uint16
	entityID    string
	entitySeq   uint64
	payload     string
	createdAt   time.Time
}

func entityPool(n int) []string {
	pool := make([]string, n)
	for i := 0; i < n; i++ {
		pool[i] = fmt.Sprintf("entity-%07d", i)
	}
	return pool
}

func partitionFor(entityID string, partitions int) uint16 {
	return uint16(uint64(murmur3.Sum32([]byte(entityID))) % uint64(partitions))
}

func buildBatch(ctx context.Context, batchSize int, partitions int, pool []string, apigwURL string) []taskRow {
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

		entityID := pool[rand.Intn(len(pool))]
		seq := nextSeq()
		partitionID := partitionFor(entityID, partitions)
		payload := fmt.Sprintf(`{"url":"https://%s/"}`, apigwURL)
		now := time.Now().UTC()

		rows = append(rows, taskRow{
			id:          taskID,
			partitionID: partitionID,
			entityID:    entityID,
			entitySeq:   seq,
			payload:     payload,
			createdAt:   now,
		})
	}
	return rows
}

func upsertBatch(ctx context.Context, db *ydb.Driver, batch []taskRow) error {
	records := make([]types.Value, 0, len(batch))
	for _, r := range batch {
		records = append(records, types.StructValue(
			types.StructFieldValue("id", types.TextValue(r.id)),
			types.StructFieldValue("partition_id", types.Uint16Value(r.partitionID)),
			types.StructFieldValue("entity_id", types.TextValue(r.entityID)),
			types.StructFieldValue("entity_seq", types.Uint64Value(r.entitySeq)),
			types.StructFieldValue("payload", types.TextValue(r.payload)),
			types.StructFieldValue("created_at", types.TimestampValueFromTime(r.createdAt)),
		))
	}

	return db.Query().Exec(ctx,
		`UPSERT INTO ordered_tasks
		SELECT id, partition_id, entity_id, entity_seq,
		       "pending"u AS status, payload,
		       0u AS attempt_count,
		       created_at
		FROM AS_TABLE($records)`,
		query.WithParameters(
			ydb.ParamsBuilder().
				Param("$records").Any(types.ListValue(records...)).
				Build(),
		),
	)
}

// Produce runs the fixed-window batch loop until ctx is cancelled.
func Produce(
	ctx context.Context,
	db *ydb.Driver,
	rate int,
	partitions int,
	entities int,
	batchWindow time.Duration,
	reportInterval time.Duration,
	ps *metrics.ProducerStats,
	apigwURL string,
) {
	if rate <= 0 {
		rate = 1
	}
	if entities <= 0 {
		entities = 1
	}

	pool := entityPool(entities)

	effectiveWindow := batchWindow
	targetBatchSize := int(math.Round(float64(rate) * batchWindow.Seconds()))
	if targetBatchSize < 1 {
		targetBatchSize = 1
		effectiveWindow = time.Duration(float64(time.Second) / float64(rate))
	}

	ps.TargetBatchSize.Set(float64(targetBatchSize))
	ps.TargetRate.Set(float64(rate))
	ps.WindowSeconds.Set(batchWindow.Seconds())

	slog.Info("producer started",
		"rate", rate,
		"partitions", partitions,
		"entities", entities,
		"batch_window", batchWindow,
		"target_batch_size", targetBatchSize,
	)

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
		batch := buildBatch(ctx, targetBatchSize, partitions, pool, apigwURL)

		if len(batch) == 0 {
			break loop
		}

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
		ps.BatchSize.Set(float64(n))

		elapsed := time.Since(windowStart)
		ps.BatchDuration.Observe(float64(elapsed.Milliseconds()))

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

	fmt.Printf("=== Producer Stats ===\n")
	fmt.Printf("total_inserted    : %d\n", total)
	fmt.Printf("rate_observed     : %.2f\n", finalRate)
	fmt.Printf("uptime            : %s\n", time.Since(ps.StartTime).Round(time.Second))
	fmt.Printf("======================\n")
}
