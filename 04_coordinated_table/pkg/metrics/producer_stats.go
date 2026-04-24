package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// ProducerStats holds Prometheus metrics for producer mode.
type ProducerStats struct {
	Registry        *prometheus.Registry
	StartTime       time.Time
	Up              prometheus.Gauge
	TargetRate      prometheus.Gauge
	WindowSeconds   prometheus.Gauge
	TargetBatchSize prometheus.Gauge
	Inserted        prometheus.Counter
	Batches         prometheus.Counter
	BatchErrors     prometheus.Counter
	Backpressure    prometheus.Counter
	ObservedRate    prometheus.Gauge
	BatchSize       prometheus.Gauge
	BatchDuration   prometheus.Histogram
}

func NewProducerStats(targetRate float64, window time.Duration) *ProducerStats {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	up := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "producer_up",
		Help: "1 while the producer is running, 0 on shutdown",
	})
	trGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "producer_target_rate",
		Help: "Configured --rate (tasks per second)",
	})
	windowSec := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "producer_window_seconds",
		Help: "Configured --batch-window in seconds",
	})
	targetBatch := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "producer_target_batch_size",
		Help: "Expected rows per batch (round(rate * window))",
	})
	inserted := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "producer_inserted_total",
		Help: "Cumulative rows successfully inserted",
	})
	batches := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "producer_batches_total",
		Help: "Cumulative batches submitted",
	})
	batchErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "producer_batch_errors_total",
		Help: "Cumulative batches that returned an error from YDB",
	})
	backpressure := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "producer_backpressure_total",
		Help: "Windows where batch duration >= configured window",
	})
	observedRate := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "producer_observed_rate",
		Help: "Rows/sec computed over the last report interval",
	})
	batchSizeHist := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "producer_batch_size",
		Help: "Rows in the last submitted batch",
	})
	batchDurHist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "producer_batch_duration_milliseconds",
		Help:    "Distribution of UPSERT latency in milliseconds",
		Buckets: []float64{8, 16, 32, 64, 128},
	})

	registry.MustRegister(up, trGauge, windowSec, targetBatch, inserted, batches, batchErrors, backpressure, observedRate, batchSizeHist, batchDurHist)

	trGauge.Set(targetRate)
	windowSec.Set(window.Seconds())

	return &ProducerStats{
		Registry:        registry,
		StartTime:       time.Now(),
		Up:              up,
		TargetRate:      trGauge,
		WindowSeconds:   windowSec,
		TargetBatchSize: targetBatch,
		Inserted:        inserted,
		Batches:         batches,
		BatchErrors:     batchErrors,
		Backpressure:    backpressure,
		ObservedRate:    observedRate,
		BatchSize:       batchSizeHist,
		BatchDuration:   batchDurHist,
	}
}
