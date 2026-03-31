package main

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/twmb/murmur3"
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
)

// produce continuously inserts task rows at the given rate (tasks/sec) into coordinated_tasks.
// ~10% of tasks get a future scheduled_at to demonstrate postpone behavior (US4).
func produce(ctx context.Context, db *ydb.Driver, rate int, partitions int) {
	if rate <= 0 {
		rate = 1
	}
	interval := time.Second / time.Duration(rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var inserted int64
	logTicker := time.NewTicker(5 * time.Second)
	defer logTicker.Stop()

	slog.Info("producer started", "rate", rate, "partitions", partitions)

	for {
		select {
		case <-ctx.Done():
			slog.Info("producer stopping", "total_inserted", inserted)
			return
		case <-logTicker.C:
			slog.Info("producer stats", "total_inserted", inserted)
		case <-ticker.C:
			if err := insertTask(ctx, db, partitions); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("insert task failed", "err", err)
				continue
			}
			inserted++
		}
	}
}

func insertTask(ctx context.Context, db *ydb.Driver, partitions int) error {
	taskID, err := generateUUID()
	if err != nil {
		return err
	}

	hash := int64(murmur3.Sum32([]byte(taskID)))
	partitionID := uint16(uint64(hash&0x7FFFFFFFFFFFFFFF) % uint64(partitions))
	priority := uint8(rand.Intn(256))
	payload := "task-payload-" + taskID

	now := time.Now().UTC()

	// US4: ~10% of tasks get a future scheduled_at (5–30s from now).
	hasScheduled := rand.Intn(10) == 0
	var scheduledAt time.Time
	if hasScheduled {
		scheduledAt = now.Add(time.Duration(5+rand.Intn(26)) * time.Second)
	}

	return db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		if hasScheduled {
			return tx.Exec(ctx,
				`UPSERT INTO coordinated_tasks (
					id, 
					hash,
					partition_id,
					priority,
					status,
					payload,
					created_at,
					scheduled_at
				)
				VALUES
					($id, $hash, $partition_id, $priority, "pending", $payload, $created_at, $scheduled_at);`,
				query.WithParameters(
					ydb.ParamsBuilder().
						Param("$id").Text(taskID).
						Param("$hash").Int64(hash).
						Param("$partition_id").Uint16(partitionID).
						Param("$priority").Uint8(priority).
						Param("$payload").Text(payload).
						Param("$created_at").Timestamp(now).
						Param("$scheduled_at").Timestamp(scheduledAt).
						Build(),
				),
			)
		}
		return tx.Exec(ctx,
			`UPSERT INTO coordinated_tasks
				(id, hash, partition_id, priority, status, payload, created_at)
			VALUES
				($id, $hash, $partition_id, $priority, "pending", $payload, $created_at);`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(taskID).
					Param("$hash").Int64(hash).
					Param("$partition_id").Uint16(partitionID).
					Param("$priority").Uint8(priority).
					Param("$payload").Text(payload).
					Param("$created_at").Timestamp(now).
					Build(),
			),
		)
	}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))
}
