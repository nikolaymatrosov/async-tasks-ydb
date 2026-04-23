package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// ProducerStats holds Prometheus metrics for producer mode.
type ProducerStats struct {
	registry        *prometheus.Registry
	startTime       time.Time
	up              prometheus.Gauge
	targetRate      prometheus.Gauge
	windowSeconds   prometheus.Gauge
	targetBatchSize prometheus.Gauge
	inserted        prometheus.Counter
	batches         prometheus.Counter
	batchErrors     prometheus.Counter
	backpressure    prometheus.Counter
	observedRate    prometheus.Gauge
	batchSize       prometheus.Histogram
	batchDuration   prometheus.Histogram
}

func newProducerStats(targetRate float64, window time.Duration) *ProducerStats {
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
	batchSizeHist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "producer_batch_size",
		Help:    "Distribution of rows per submitted batch",
		Buckets: []float64{1, 10, 100, 1000, 10000},
	})
	batchDurHist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "producer_batch_duration_seconds",
		Help:    "Distribution of UPSERT latency",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	})

	registry.MustRegister(up, trGauge, windowSec, targetBatch, inserted, batches, batchErrors, backpressure, observedRate, batchSizeHist, batchDurHist)

	trGauge.Set(targetRate)
	windowSec.Set(window.Seconds())

	return &ProducerStats{
		registry:        registry,
		startTime:       time.Now(),
		up:              up,
		targetRate:      trGauge,
		windowSeconds:   windowSec,
		targetBatchSize: targetBatch,
		inserted:        inserted,
		batches:         batches,
		batchErrors:     batchErrors,
		backpressure:    backpressure,
		observedRate:    observedRate,
		batchSize:       batchSizeHist,
		batchDuration:   batchDurHist,
	}
}
