package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topicoptions"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topictypes"
	yc "github.com/ydb-platform/ydb-go-yc"
)

// cdcMessage is the JSON structure emitted by YDB changefeeds in NEW_IMAGE mode.
// INSERT events have no "update" key; UPDATE/DELETE events include "update".
type cdcMessage struct {
	Key      []string        `json:"key"`
	Update   json.RawMessage `json:"update,omitempty"` // present only on UPDATE/DELETE
	NewImage json.RawMessage `json:"newImage"`
}

// taskRow represents the task table schema from newImage.
type taskRow struct {
	ID        string `json:"id"`
	Payload   string `json:"payload"`
	CreatedAt string `json:"created_at"`
	DoneAt    *string `json:"done_at"`
}

func main() {
	workDelay   := flag.Duration("work-delay", 100*time.Millisecond, "Simulated processing delay per message")
	topicSuffix := flag.String("topic", "tasks/cdc_tasks", "Topic path relative to the database root (db.Name() is prepended automatically)")
	consumer    := flag.String("consumer", "cdc-worker", "Topic consumer name")
	flag.Parse()

	endpoint := os.Getenv("YDB_ENDPOINT")
	if endpoint == "" {
		log.Fatal("YDB_ENDPOINT is not set")
	}
	saKeyFile := os.Getenv("YDB_SA_KEY_FILE")
	if saKeyFile == "" {
		log.Fatal("YDB_SA_KEY_FILE is not set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := ydb.Open(ctx, endpoint,
		yc.WithServiceAccountKeyFileCredentials(saKeyFile),
		yc.WithInternalCA(),
	)
	if err != nil {
		log.Fatalf("ydb.Open: %v", err)
	}
	defer db.Close(context.Background())

	// Construct the full absolute topic path: db.Name() + "/" + suffix.
	topicPath := db.Name() + "/" + *topicSuffix

	// Ensure the consumer exists on the topic. AlreadyExists is not an error.
	alterErr := db.Topic().Alter(ctx, topicPath,
		topicoptions.AlterWithAddConsumers(topictypes.Consumer{
			Name:            *consumer,
			SupportedCodecs: []topictypes.Codec{topictypes.CodecRaw},
		}),
	)
	if alterErr != nil && !ydb.IsOperationErrorAlreadyExistsError(alterErr) {
		log.Fatalf("failed to register consumer %q on topic %q: %v", *consumer, topicPath, alterErr)
	}

	var (
		processed atomic.Int64
		skipped   atomic.Int64
		errors    atomic.Int64
	)

	// Stats goroutine: log counters every second.
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var prevProcessed, prevSkipped int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p := processed.Load()
				s := skipped.Load()
				e := errors.Load()
				rate := float64(p-prevProcessed) + float64(s-prevSkipped)
				prevProcessed = p
				prevSkipped = s
				log.Printf("[stats] processed=%d skipped=%d errors=%d rate=%.1f msg/s",
					p, s, e, rate)
			}
		}
	}()

	log.Printf("CDC worker started: topic=%s consumer=%s work-delay=%s. Press Ctrl-C to stop.",
		topicPath, *consumer, *workDelay)

	// Open a long-lived reader. The reader handles reconnects internally.
	reader, err := db.Topic().StartReader(*consumer, topicoptions.ReadTopic(topicPath))
	if err != nil {
		log.Fatalf("failed to start topic reader: %v", err)
	}
	defer reader.Close(ctx)

	// Read-process-commit loop (at-least-once).
	// UPSERT is idempotent so reprocessing on failure is safe.
	for {
		if ctx.Err() != nil {
			break
		}

		batch, err := reader.ReadMessagesBatch(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			errors.Add(1)
			log.Printf("ReadMessagesBatch error: %v", err)
			continue
		}

		// Collect task rows to process (INSERTs and UPDATEs).
		// Skip DELETE events only (they have newImage == null).
		type rowToProcess struct {
			id        uuid.UUID
			createdAt string
		}
		var rowsToProcess []rowToProcess

		for _, msg := range batch.Messages {
			data, err := io.ReadAll(msg)
			if err != nil {
				errors.Add(1)
				continue
			}

			var cdc cdcMessage
			if err := json.Unmarshal(data, &cdc); err != nil {
				// Unparseable — skip silently.
				skipped.Add(1)
				continue
			}

			// Skip DELETE events (newImage is null or empty in DELETE operations).
			// Process both INSERTs (no update field) and UPDATEs (has update field).
			if len(cdc.NewImage) == 0 {
				skipped.Add(1)
				continue
			}

			if len(cdc.Key) == 0 {
				skipped.Add(1)
				continue
			}
			rowID, err := uuid.Parse(cdc.Key[0])
			if err != nil {
				skipped.Add(1)
				continue
			}

			// Parse newImage to extract created_at
			var row taskRow
			if err := json.Unmarshal(cdc.NewImage, &row); err != nil {
				skipped.Add(1)
				continue
			}

			rowsToProcess = append(rowsToProcess, rowToProcess{
				id:        rowID,
				createdAt: row.CreatedAt,
			})
		}

		// Emulate work for each row and mark done.
		for _, r := range rowsToProcess {
			time.Sleep(*workDelay)

			// Parse created_at timestamp from CDC message
			createdAtTime, err := time.Parse(time.RFC3339Nano, r.createdAt)
			if err != nil {
				errors.Add(1)
				log.Printf("Failed to parse created_at for id=%s: %v", r.id, err)
				continue
			}

			// Mark task done via UPSERT, preserving the created_at value (idempotent).
			upsertErr := db.Query().Exec(ctx,
				`UPSERT INTO tasks (id, payload, created_at, done_at) VALUES ($id, '', $created_at, CurrentUtcTimestamp())`,
				query.WithParameters(
					ydb.ParamsBuilder().
						Param("$id").Uuid(r.id).
						Param("$created_at").Timestamp(createdAtTime).
						Build(),
				),
			)
			if upsertErr != nil {
				if ctx.Err() != nil {
					break
				}
				errors.Add(1)
				log.Printf("UPSERT error for id=%s: %v", r.id, upsertErr)
				continue
			}
			processed.Add(1)
		}

		// Commit the whole batch offset regardless of per-message errors —
		// we don't want to re-read the same batch forever on transient UPSERT errors.
		if err := reader.Commit(ctx, batch); err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("Commit error: %v", err)
		}
	}

	// Context cancellation is the normal shutdown path.

	// Final stats.
	fmt.Printf("\n--- Final Stats ---\n")
	fmt.Printf("Processed (INSERTs) : %d\n", processed.Load())
	fmt.Printf("Skipped (non-INSERT): %d\n", skipped.Load())
	fmt.Printf("Errors              : %d\n", errors.Load())
}
