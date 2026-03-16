package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topicoptions"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topicwriter"
	yc "github.com/ydb-platform/ydb-go-yc"

	"github.com/google/uuid"
	"github.com/twmb/murmur3"
)

// TaskMessage is the payload written to the YDB topic.
type TaskMessage struct {
	ID        string    `json:"id"`
	Payload   []byte    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

// generateMessage creates a TaskMessage with random content and returns its JSON encoding.
func generateMessage() []byte {
	payload := make([]byte, 64)
	if _, err := rand.Read(payload); err != nil {
		slog.Error("rand.Read failed", "err", err)
		os.Exit(1)
	}
	msg := TaskMessage{
		ID:        uuid.New().String(),
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		slog.Error("json.Marshal failed", "err", err)
		os.Exit(1)
	}
	return b
}

// safeWriter wraps a single topicwriter.Writer with exponential-backoff retry.
type safeWriter struct {
	w           *topicwriter.Writer
	partitionID int64
}

// Write sends messages with exponential backoff retry.
// Transport errors are retried up to 5 min total; ErrQueueLimitExceed retries indefinitely;
// all other errors are returned immediately.
func (w *safeWriter) Write(ctx context.Context, messages []topicwriter.Message) error {
	const (
		initialInterval = time.Second
		multiplier      = 1.5
		maxInterval     = 30 * time.Second
		maxElapsed      = 5 * time.Minute
	)

	interval := initialInterval
	start := time.Now()

	for {
		err := w.w.Write(ctx, messages...)
		if err == nil {
			return nil
		}

		// Context cancelled — surface immediately.
		if ctx.Err() != nil {
			return ctx.Err()
		}


		// Transport error — retryable within elapsed cap.
		if ydb.IsTransportError(err) {
			if time.Since(start) >= maxElapsed {
				return fmt.Errorf("max elapsed time exceeded after transport errors: %w", err)
			}
			slog.Warn("transport error, retrying", "err", err, "retry_in", interval, "partition_id", w.partitionID)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}
			interval = time.Duration(float64(interval) * multiplier)
			if interval > maxInterval {
				interval = maxInterval
			}
			continue
		}

		// Permanent error — surface immediately.
		return err
	}
}

// Producer manages per-partition writers for a single YDB topic.
type Producer struct {
	db         *ydb.Driver
	topic      string
	partitions []int64
	writers    map[int64]*safeWriter
}

// NewProducer creates a new Producer for the given topic path.
func NewProducer(db *ydb.Driver, topicPath string) *Producer {
	return &Producer{
		db:    db,
		topic: topicPath,
	}
}

// Start enumerates active partitions and opens one pinned writer per partition.
func (p *Producer) Start(ctx context.Context) error {
	if p.writers != nil {
		panic("producer already started")
	}

	desc, err := p.db.Topic().Describe(ctx, p.topic)
	if err != nil {
		return fmt.Errorf("Topic().Describe: %w", err)
	}

	p.writers = make(map[int64]*safeWriter, len(desc.Partitions))
	p.partitions = make([]int64, 0, len(desc.Partitions))

	for _, part := range desc.Partitions {
		if !part.Active {
			continue
		}
		id := part.PartitionID
		w, err := p.db.Topic().StartWriter(p.topic,
			topicoptions.WithWriterPartitionID(id),
			topicoptions.WithWriterWaitServerAck(true),
		)
		if err != nil {
			// Partial cleanup on failure.
			_ = p.Stop(context.Background())
			return fmt.Errorf("StartWriter partition %d: %w", id, err)
		}
		p.partitions = append(p.partitions, id)
		p.writers[id] = &safeWriter{w: w, partitionID: id}
	}

	slog.Info("producer started", "topic", p.topic, "partitions", len(p.partitions))
	return nil
}

// Stop closes all partition writers and collects errors.
func (p *Producer) Stop(ctx context.Context) error {
	var errs []error
	for id, sw := range p.writers {
		if err := sw.w.Close(ctx); err != nil {
			slog.Error("failed to close writer", "partition_id", id, "err", err)
			errs = append(errs, err)
		}
		delete(p.writers, id)
	}
	slog.Info("producer stopped")
	return errors.Join(errs...)
}

// hashKey maps a string key to a partition index using Murmur3 32-bit hash.
func hashKey(key string, numPartitions int) int {
	return int(murmur3.Sum32([]byte(key)) % uint32(numPartitions))
}

// Write routes messages to the partition determined by partitionKey.
func (p *Producer) Write(ctx context.Context, partitionKey string, messages ...topicwriter.Message) error {
	if p.writers == nil {
		return errors.New("producer not started")
	}
	idx := hashKey(partitionKey, len(p.partitions))
	partitionID := p.partitions[idx]
	return p.writers[partitionID].Write(ctx, messages)
}

func main() {
	topicFlag := flag.String("topic", "tasks/direct", "Topic path relative to the database root")
	messagesFlag := flag.Int("messages", 10, "Number of messages to publish per key")
	flag.Parse()

	// Configure structured JSON logging.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	endpoint := os.Getenv("YDB_ENDPOINT")
	if endpoint == "" {
		slog.Error("YDB_ENDPOINT is not set")
		os.Exit(1)
	}
	saKeyFile := os.Getenv("YDB_SA_KEY_FILE")
	if saKeyFile == "" {
		slog.Error("YDB_SA_KEY_FILE is not set")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := ydb.Open(ctx, endpoint,
		yc.WithServiceAccountKeyFileCredentials(saKeyFile),
		yc.WithInternalCA(),
	)
	if err != nil {
		slog.Error("ydb.Open failed", "err", err)
		os.Exit(1)
	}
	defer db.Close(context.Background())

	topicPath := db.Name() + "/" + *topicFlag

	producer := NewProducer(db, topicPath)

	if err := producer.Start(ctx); err != nil {
		slog.Error("producer.Start failed", "err", err)
		os.Exit(1)
	}
	defer producer.Stop(context.Background()) //nolint:errcheck

	// Demo loop: two partition keys, messagesFlag messages each.
	keys := []string{"user-42", "order-99"}
	var totalWritten int
	partitionsUsed := make(map[int64]struct{})

	for _, key := range keys {
		for i := 1; i <= *messagesFlag; i++ {
			payload := generateMessage()
			msg := topicwriter.Message{Data: bytes.NewReader(payload)}

			// Determine which partition this key maps to (for logging).
			idx := hashKey(key, len(producer.partitions))
			partitionID := producer.partitions[idx]
			partitionsUsed[partitionID] = struct{}{}

			if err := producer.Write(ctx, key, msg); err != nil {
				slog.Error("write failed", "partition_key", key, "msg_index", i, "err", err)
				continue
			}

			slog.Info("message written",
				"partition_key", key,
				"partition_id", partitionID,
				"msg_index", i,
			)
			totalWritten++
		}
	}

	// Final stats.
	fmt.Printf("\n--- Stats ---\n")
	fmt.Printf("Messages written : %d\n", totalWritten)
	fmt.Printf("Keys used        : %d\n", len(keys))
	fmt.Printf("Partitions used  : %d\n", len(partitionsUsed))
}
