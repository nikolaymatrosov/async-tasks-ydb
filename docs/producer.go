package database

import (
	"context"
	"path"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topicoptions"
	"github.com/ydb-platform/ydb-go-sdk/v3/topic/topicwriter"

	"bb.yandex-team.ru/cloud/cloud-go/serverless/ycmail/internal/diagnostic/tracing"
	"bb.yandex-team.ru/cloud/cloud-go/serverless/ycmail/internal/errors"
	"bb.yandex-team.ru/cloud/cloud-go/serverless/ycmail/internal/log"
	"bb.yandex-team.ru/cloud/cloud-go/serverless/ycmail/internal/utils"
)

const (
	cffggMaxBackOffInterval    = 30 * time.Second
	cffggMaxBackOffElapsedTime = 5 * time.Minute

	sendErrExperiment = "write_error"
)

type Producer struct {
	db *ydb.Driver

	topic string

	partitions []int64
	writers    map[int64]*safeWriter
	opts       []topicoptions.WriterOption

	log *log.Logger
}

type TopicName string

type ProducerFactory func(topic TopicName) *Producer

func NewProducerFactory(db *ydb.Driver) ProducerFactory {
	// NOTE: don't use it directly, it doesn't track uniqueness

	return func(topic TopicName) *Producer {
		return NewProducer(db, topic)
	}
}

func NewProducer(db *ydb.Driver, topic TopicName) *Producer {
	return &Producer{
		db:    db,
		topic: path.Join(db.prefix, topic.String()),
		log:   log.Named("producer/" + topic.String()),
	}
}

func (p *Producer) Start(ctx context.Context) error {
	if p.writers != nil {
		panic("producer is already started")
	}

	desc, err := p.db.Topic().Describe(ctx, p.topic)
	if err != nil {
		return errors.EnrichFromContext(ctx, err)
	}

	p.writers = make(map[int64]*safeWriter, len(desc.Partitions))

	for _, partition := range desc.Partitions {
		if !partition.Active {
			continue
		}

		opts := append(p.opts,
			topicoptions.WithWriterPartitionID(partition.PartitionID),
			topicoptions.WithWriterWaitServerAck(true),
			topicoptions.WithWriterCheckRetryErrorFunction(
				func(errInfo topicoptions.CheckErrorRetryArgs) topicoptions.CheckErrorRetryResult {
					isTransportError := ydb.IsTransportError(errInfo.Error)

					// Retry any write errors.
					// In the worst case, there will be a call to the duty, but it is better than to lose some billing data.
					p.log.Info("topic writer error", log.Err(errInfo.Error), log.Bool("retry", isTransportError), log.F.Experiment(sendErrExperiment))

					// Retry for all transport errors
					if isTransportError {
						return topicoptions.CheckErrorRetryDecisionRetry
					}

					return topicoptions.CheckErrorRetryDecisionDefault
				}),
		)

		w, err := p.db.Topic().StartWriter(p.topic, opts...)

		if err != nil {
			_ = p.Stop(ctx)
			return errors.EnrichFromContext(ctx, err)
		}

		// Don't call WaitInit here as if we have a lot of partitions we will wait here for connection to all
		// but some of them (or all) can be not used.
		// So we put first connection logic to the first call

		p.partitions = append(p.partitions, partition.PartitionID)
		p.writers[partition.PartitionID] = &safeWriter{
			w: w,
		}
	}

	return nil
}

func (p *Producer) Stop(ctx context.Context) error {
	var merr []error
	for k, w := range p.writers {
		merr = append(merr, w.Close(ctx))
		delete(p.writers, k)
	}

	return errors.EnrichFromContext(ctx, errors.Join(merr...))
}

func (p *Producer) Write(ctx context.Context, partitionKey string, messages ...topicwriter.Message) (err error) {
	span, ctx := tracing.StartChildSpan(ctx, utils.CallerMethod())
	defer func() { tracing.FinishSpan(span, err) }()

	if p.writers == nil {
		return errors.New("not started")
	}

	hash := p.hashKey(partitionKey)
	partitionID := p.partitions[hash%len(p.partitions)]

	w := p.writers[partitionID]

	return w.Write(ctx, messages, func(err error) {
		p.log.Warn("can't write message",
			log.Err(err),
			log.String("partition_key", partitionKey),
			log.String("topic", p.topic),
			log.Int64("partition_id", partitionID))
	})
}

func (p *Producer) hashKey(key string) int {
	// FVN-1a implementation:
	// https://en.wikipedia.org/wiki/Fowler–Noll–Vo_hash_function

	const (
		uint64Offset uint64 = 0xcbf29ce484222325
		uint64Prime  uint64 = 0x00000100000001b3
	)

	hash := uint64Offset

	for _, b := range []byte(key) {
		hash ^= uint64(b)
		hash *= uint64Prime
	}

	result := int(hash)
	if result < 0 {
		return -result
	}
	return result
}

// TODO: remove, works without lock
type safeWriter struct {
	w *topicwriter.Writer
	//mu sync.Mutex
}

func (w *safeWriter) Write(ctx context.Context, messages []topicwriter.Message, logErr func(error)) error {
	// Process ErrQueueLimitExceed
	for {
		b := backoff.NewExponentialBackOff()
		b.MaxInterval = cffggMaxBackOffInterval

		_, err := backoff.Retry(ctx, func() (struct{}, error) {
			return struct{}{}, w.tryWrite(ctx, messages, logErr)
		},
			backoff.WithBackOff(b),
			backoff.WithMaxElapsedTime(cffggMaxBackOffElapsedTime),
		)

		if errors.Is(err, topicwriter.ErrQueueLimitExceed) {
			continue
		}

		return errors.EnrichFromContext(ctx, err)
	}
}

func (w *safeWriter) Close(ctx context.Context) error {
	return errors.EnrichFromContext(ctx, w.w.Close(ctx))
}

func (w *safeWriter) tryWrite(ctx context.Context, messages []topicwriter.Message, logErr func(error)) error {
	//w.mu.Lock()
	//defer w.mu.Unlock()

	err := w.w.Write(ctx, messages...)

	if err != nil {
		if ctx.Err() != nil {
			return backoff.Permanent(ctx.Err())
		}

		logErr(err)

		if errors.Is(topicwriter.ErrQueueLimitExceed, err) {
			return err
		}

		return backoff.Permanent(err)
	}

	return nil
}

func (tp TopicName) String() string {
	return string(tp)
}
