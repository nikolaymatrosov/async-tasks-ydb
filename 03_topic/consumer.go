package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/types"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topicoptions"
)

// Consumer reads messages from YDB topics and executes benchmark scenarios.
type Consumer struct {
	db *ydb.Driver
}

// NewConsumer creates a Consumer backed by the given YDB driver.
func NewConsumer(db *ydb.Driver) *Consumer {
	return &Consumer{db: db}
}

// RunScenario consumes messages from topic using consumerName, spawning one goroutine
// per partition. It calls workload for each message and stops when target messages have
// been processed. Returns ScenarioResult with wall-clock duration and throughput.
//
// tliCounter may be nil (for noop or insert-only workloads); when non-nil its value is
// read at the end to populate ScenarioResult.TLIErrors before logging.
func (c *Consumer) RunScenario(
	ctx context.Context,
	name, topic, consumerName string,
	partitionCount int,
	target int64,
	tliCounter *atomic.Int64,
	workload func(context.Context, BenchMessage) error,
) (ScenarioResult, error) {
	fmt.Print("\n\n")
	slog.Info("scenario started", "scenario", name)
	start := time.Now()

	var counter atomic.Int64

	live := NewLiveStats(name, target, &counter, tliCounter)
	live.Start()

	scenarioCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, partitionCount)

	for i := 0; i < partitionCount; i++ {
		wg.Add(1)
		go func(partitionID int) {
			defer wg.Done()
			if err := c.runPartitionReader(scenarioCtx, topic, consumerName, partitionID, target, &counter, cancel, workload); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	live.Stop()
	close(errs)

	duration := time.Since(start)
	messages := counter.Load()

	var firstErr error
	for err := range errs {
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil && ctx.Err() == nil {
		return ScenarioResult{}, fmt.Errorf("scenario %q: %w", name, firstErr)
	}

	var tliErrors int64
	if tliCounter != nil {
		tliErrors = tliCounter.Load()
	}

	var msgPerSec float64
	if duration.Seconds() > 0 {
		msgPerSec = float64(messages) / duration.Seconds()
	}

	result := ScenarioResult{
		Name:      name,
		Messages:  messages,
		TLIErrors: tliErrors,
		Duration:  duration,
		MsgPerSec: msgPerSec,
	}

	slog.Info("scenario complete",
		"scenario", name,
		"messages", result.Messages,
		"tli_errors", result.TLIErrors,
		"duration_s", result.Duration.Seconds(),
	)

	return result, nil
}

func (c *Consumer) runPartitionReader(
	ctx context.Context,
	topic, consumerName string,
	partitionID int,
	target int64,
	counter *atomic.Int64,
	cancel context.CancelFunc,
	workload func(context.Context, BenchMessage) error,
) error {
	reader, err := c.db.Topic().StartReader(
		consumerName,
		topicoptions.ReadSelectors{{Path: topic, Partitions: []int64{int64(partitionID)}}},
	)
	if err != nil {
		return fmt.Errorf("StartReader partition %d: %w", partitionID, err)
	}
	defer reader.Close(context.Background()) //nolint:errcheck

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("ReadMessage partition %d: %w", partitionID, err)
		}

		var benchMsg BenchMessage
		if err := json.NewDecoder(msg).Decode(&benchMsg); err != nil {
			// Skip malformed messages.
			_ = reader.Commit(ctx, msg)
			continue
		}

		if err := workload(ctx, benchMsg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("workload: %w", err)
		}

		_ = reader.Commit(ctx, msg)

		if counter.Add(1) >= target {
			cancel()
			return nil
		}
	}
}

// statsWorkload returns a workload function that increments the counter for
// msg.Type using a serializable interactive transaction so that the
// UPSERT…SELECT…LEFT JOIN read-modify-write is consistent.
func statsWorkload(db *ydb.Driver, _ *atomic.Int64) func(context.Context, BenchMessage) error {
	return func(ctx context.Context, msg BenchMessage) error {
		var a, b, c int64
		switch msg.Type {
		case "A":
			a = 1
		case "B":
			b = 1
		case "C":
			c = 1
		}

		return db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
			return tx.Exec(ctx,
				`UPSERT INTO stats
				 SELECT
				     r.user_id AS user_id,
				     COALESCE(t.a, 0) + r.a AS a,
				     COALESCE(t.b, 0) + r.b AS b,
				     COALESCE(t.c, 0) + r.c AS c
				 FROM AS_TABLE($records) AS r
				 LEFT JOIN stats AS t ON r.user_id = t.user_id`,
				query.WithParameters(
					ydb.ParamsBuilder().
						Param("$records").Any(types.ListValue(
							types.StructValue(
								types.StructFieldValue("user_id", types.UuidValue(msg.UserID)),
								types.StructFieldValue("a", types.Int64Value(a)),
								types.StructFieldValue("b", types.Int64Value(b)),
								types.StructFieldValue("c", types.Int64Value(c)),
							),
						)).
						Build(),
				),
				query.WithTxControl(query.TxControl(query.CommitTx())),
			)
		}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))
	}
}

// processedWorkload returns a workload function that inserts msg.ID into the
// processed table using an idempotent UPSERT — no preceding read, no TLI.
func processedWorkload(db *ydb.Driver) func(context.Context, BenchMessage) error {
	return func(ctx context.Context, msg BenchMessage) error {
		return db.Query().Exec(ctx,
			`UPSERT INTO processed (id) VALUES ($id)`,
			query.WithParameters(
				ydb.ParamsBuilder().Param("$id").Uuid(msg.ID).Build(),
			),
		)
	}
}
