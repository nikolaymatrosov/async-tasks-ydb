package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Stats holds Prometheus metrics shared between the worker and the display loop.
type Stats struct {
	workerID  string
	startTime time.Time
	Registry  *prometheus.Registry
	Processed prometheus.Counter
	Locked    prometheus.Counter
	Errors    prometheus.Counter
	Partitions prometheus.Gauge
	up         prometheus.Gauge
}

func NewStats(workerID string) *Stats {
	registry := prometheus.NewRegistry()

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	processed := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "coordinator_tasks_processed_total",
		Help:        "Cumulative tasks marked completed by this worker",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	locked := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "coordinator_tasks_locked_total",
		Help:        "Cumulative tasks locked (includes retries)",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	errors := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "coordinator_tasks_errors_total",
		Help:        "Cumulative failed lock or complete operations",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	partitions := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "coordinator_partitions_owned",
		Help:        "Current number of partitions owned by this worker",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	up := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "coordinator_up",
		Help:        "1 if the worker process is running, 0 otherwise",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})

	registry.MustRegister(processed, locked, errors, partitions, up)
	up.Set(1)

	return &Stats{
		workerID:   workerID,
		startTime:  time.Now(),
		Registry:   registry,
		Processed:  processed,
		Locked:     locked,
		Errors:     errors,
		Partitions: partitions,
		up:         up,
	}
}

func ReadCounter(c prometheus.Counter) int64 {
	var m dto.Metric
	_ = c.Write(&m)
	return int64(m.GetCounter().GetValue())
}

func ReadGauge(g prometheus.Gauge) int64 {
	var m dto.Metric
	_ = g.Write(&m)
	return int64(m.GetGauge().GetValue())
}

// Display prints a periodic stats block every 5 seconds until ctx is done.
func (s *Stats) Display(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.print()
		}
	}
}

func (s *Stats) print() {
	uptime := time.Since(s.startTime).Round(time.Second)
	partitions := ReadGauge(s.Partitions)
	processed := ReadCounter(s.Processed)
	locked := ReadCounter(s.Locked)
	errors := ReadCounter(s.Errors)

	slog.Info("worker stats",
		"worker_id", s.workerID,
		"partitions_owned", partitions,
		"tasks_processed", processed,
		"tasks_locked", locked,
		"tasks_errors", errors,
		"uptime", uptime.String(),
	)

	fmt.Printf("=== Worker %s Stats ===\n", s.workerID[:8])
	fmt.Printf("Partitions owned: %6d\n", partitions)
	fmt.Printf("Tasks processed:  %6d\n", processed)
	fmt.Printf("Tasks locked:     %6d\n", locked)
	fmt.Printf("Tasks errors:     %6d\n", errors)
	fmt.Printf("Uptime:           %6s\n", uptime)
	fmt.Printf("========================\n")
}
