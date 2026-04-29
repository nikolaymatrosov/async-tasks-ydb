package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	dto "github.com/prometheus/client_model/go"
)

// Stats holds Prometheus metrics shared between the worker and the display loop.
type Stats struct {
	workerID   string
	startTime  time.Time
	Registry   *prometheus.Registry
	Processed  prometheus.Counter
	Locked     prometheus.Counter
	Errors     prometheus.Counter
	Failed     prometheus.Counter
	Backoffs   prometheus.Counter
	Partitions prometheus.Gauge
	APIGWCalls *prometheus.CounterVec
	up         prometheus.Gauge

	blockedMu     sync.Mutex
	blockedReason map[string]string // entity_id -> "backoff" | "terminal"
}

func NewStats(workerID string) *Stats {
	registry := prometheus.NewRegistry()

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	processed := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ordered_tasks_processed_total",
		Help:        "Cumulative tasks marked completed by this worker",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	locked := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ordered_tasks_locked_total",
		Help:        "Cumulative tasks locked (includes retries)",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	errors := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ordered_tasks_errors_total",
		Help:        "Cumulative failed lock or complete operations",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	failed := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ordered_tasks_terminally_failed_total",
		Help:        "Cumulative tasks that exhausted retries and reached status=failed",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	backoffs := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ordered_tasks_backoff_total",
		Help:        "Cumulative MarkFailedWithBackoff calls (transient retries)",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	partitions := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "ordered_partitions_owned",
		Help:        "Current number of partitions owned by this worker",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})
	up := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "ordered_up",
		Help:        "1 if the worker process is running, 0 otherwise",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	})

	apigwCalls := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "ordered_apigw_calls_total",
		Help:        "HTTP calls made to the API Gateway, by response status",
		ConstLabels: prometheus.Labels{"worker_id": workerID},
	}, []string{"http_status"})

	registry.MustRegister(processed, locked, errors, failed, backoffs, partitions, up, apigwCalls)
	up.Set(1)

	return &Stats{
		workerID:      workerID,
		startTime:     time.Now(),
		Registry:      registry,
		Processed:     processed,
		Locked:        locked,
		Errors:        errors,
		Failed:        failed,
		Backoffs:      backoffs,
		Partitions:    partitions,
		APIGWCalls:    apigwCalls,
		up:            up,
		blockedReason: make(map[string]string),
	}
}

// RecordBlocked stamps the entity's current blocked reason ("backoff" or "terminal").
// The map is bounded by active entity count; callers should clear via ClearBlocked
// when the entity is no longer blocked.
func (s *Stats) RecordBlocked(entityID, reason string) {
	s.blockedMu.Lock()
	s.blockedReason[entityID] = reason
	s.blockedMu.Unlock()
}

func (s *Stats) ClearBlocked(entityID string) {
	s.blockedMu.Lock()
	delete(s.blockedReason, entityID)
	s.blockedMu.Unlock()
}

func (s *Stats) BlockedSnapshot() map[string]string {
	s.blockedMu.Lock()
	defer s.blockedMu.Unlock()
	out := make(map[string]string, len(s.blockedReason))
	for k, v := range s.blockedReason {
		out[k] = v
	}
	return out
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

// PrintFinal renders the shutdown stats block including blocked entities.
func (s *Stats) PrintFinal() {
	s.print()
	blocked := s.BlockedSnapshot()
	if len(blocked) == 0 {
		return
	}
	fmt.Printf("=== Blocked entities (%d) ===\n", len(blocked))
	terminal := 0
	backoff := 0
	for _, reason := range blocked {
		switch reason {
		case "terminal":
			terminal++
		case "backoff":
			backoff++
		}
	}
	fmt.Printf("  terminal : %6d\n", terminal)
	fmt.Printf("  backoff  : %6d\n", backoff)
	fmt.Printf("============================\n")
}

func (s *Stats) print() {
	uptime := time.Since(s.startTime).Round(time.Second)
	partitions := ReadGauge(s.Partitions)
	processed := ReadCounter(s.Processed)
	locked := ReadCounter(s.Locked)
	errors := ReadCounter(s.Errors)
	failed := ReadCounter(s.Failed)
	backoffs := ReadCounter(s.Backoffs)

	slog.Info("worker stats",
		"worker_id", s.workerID,
		"partitions_owned", partitions,
		"tasks_processed", processed,
		"tasks_locked", locked,
		"tasks_errors", errors,
		"tasks_terminally_failed", failed,
		"tasks_backoff", backoffs,
		"uptime", uptime.String(),
	)

	fmt.Printf("=== Worker %s Stats ===\n", s.workerID[:8])
	fmt.Printf("Partitions owned:        %6d\n", partitions)
	fmt.Printf("Tasks processed:         %6d\n", processed)
	fmt.Printf("Tasks locked:            %6d\n", locked)
	fmt.Printf("Tasks errors:            %6d\n", errors)
	fmt.Printf("Tasks backoff:           %6d\n", backoffs)
	fmt.Printf("Tasks terminally failed: %6d\n", failed)
	fmt.Printf("Uptime:                  %6s\n", uptime)
	fmt.Printf("=============================\n")
}
