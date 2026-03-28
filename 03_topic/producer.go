package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/murmur3"
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topicoptions"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topicwriter"
)

// hashKey maps a string key to a partition index using Murmur3 64-bit hash.
func hashKey(key string, partitions int) int64 {
	return int64(murmur3.Sum64([]byte(key)) % uint64(partitions))
}

// safeWriter wraps a single partition writer with a mutex for concurrent-safe writes.
type safeWriter struct {
	mu sync.Mutex
	w  *topicwriter.Writer
}

func (sw *safeWriter) write(ctx context.Context, data []byte) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(ctx, topicwriter.Message{Data: bytes.NewReader(data)})
}

// Producer generates and publishes BenchMessages to YDB topics.
type Producer struct {
	db *ydb.Driver
}

// NewProducer creates a new Producer backed by the given YDB driver.
func NewProducer(db *ydb.Driver) *Producer {
	return &Producer{db: db}
}

// Generate creates n BenchMessages with sampled UserIDs and round-robin Types.
func (p *Producer) Generate(n, users int, sampler *UserIDSampler) []BenchMessage {
	_ = users // users count is encoded in the sampler
	types := []string{"A", "B", "C"}
	messages := make([]BenchMessage, n)
	for i := range messages {
		messages[i] = BenchMessage{
			ID:     uuid.New(),
			UserID: sampler.Sample(),
			Type:   types[i%3],
		}
	}
	return messages
}

// Publish writes all messages to topicPath, routing each to the partition determined
// by hashKey(keyFn(msg), 10). Writers are flushed and closed on completion.
// It displays live progress via LiveStats and returns a ProducerResult.
// lineOffset controls which pre-allocated line above the cursor to use (0 = current line).
func (p *Producer) Publish(ctx context.Context, name string, messages []BenchMessage, topicPath string, lineOffset int, keyFn func(BenchMessage) string) (ProducerResult, error) {
	const partitions = 10

	writers := make([]*safeWriter, partitions)
	for i := range writers {
		w, err := p.db.Topic().StartWriter(topicPath,
			topicoptions.WithWriterPartitionID(int64(i)),
			topicoptions.WithWriterWaitServerAck(true),
		)
		if err != nil {
			for j := 0; j < i; j++ {
				_ = writers[j].w.Close(context.Background())
			}
			return ProducerResult{}, fmt.Errorf("StartWriter partition %d: %w", i, err)
		}
		writers[i] = &safeWriter{w: w}
	}
	defer func() {
		for _, sw := range writers {
			_ = sw.w.Close(context.Background())
		}
	}()

	slog.Info("producer started", "topic", topicPath, "partitions", partitions)

	var counter atomic.Int64
	live := NewLiveStatsAt(name, int64(len(messages)), &counter, nil, lineOffset)
	live.Start()

	start := time.Now()
	for i := range messages {
		key := keyFn(messages[i])
		partitionIdx := hashKey(key, partitions)
		data, err := json.Marshal(messages[i])
		if err != nil {
			live.Stop()
			return ProducerResult{}, fmt.Errorf("json.Marshal message %d: %w", i, err)
		}
		if err := writers[partitionIdx].write(ctx, data); err != nil {
			live.Stop()
			return ProducerResult{}, fmt.Errorf("write to partition %d: %w", partitionIdx, err)
		}
		counter.Add(1)
	}
	live.Stop()
	duration := time.Since(start)

	slog.Info("publish complete", "topic", topicPath, "messages", len(messages))

	n := int64(len(messages))
	var msgPerSec float64
	if duration.Seconds() > 0 {
		msgPerSec = float64(n) / duration.Seconds()
	}
	return ProducerResult{
		Name:      name,
		Messages:  n,
		Duration:  duration,
		MsgPerSec: msgPerSec,
	}, nil
}
