// Package main implements a self-contained HTTP target that validates
// per-entity ordinal monotonicity and supports configurable HTTP 429 / 5xx
// fault injection. Used as the dispatch destination for the
// 05_ordered_tasks/cmd/worker example so its per-entity ordering guarantee can
// be exercised end-to-end.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/twmb/murmur3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	maxBodyBytes = 64 * 1024
	numBuckets   = 64
)

type entityState struct {
	LastAcceptedSeq uint64
	Accepted        uint64
	LastAcceptedAt  time.Time
}

type shard struct {
	mu sync.Mutex
	m  map[string]*entityState
}

type server struct {
	startTime       time.Time
	listen          string
	fault429Percent int
	fault5xxPercent int

	shards [numBuckets]*shard

	registry         *prometheus.Registry
	acceptedTotal    *prometheus.CounterVec
	duplicateTotal   *prometheus.CounterVec
	violationTotal   *prometheus.CounterVec
	faultInjected    *prometheus.CounterVec
	requestDuration  prometheus.Histogram
	faultPercentGge  *prometheus.GaugeVec
	requestsServed   atomic.Uint64
	acceptedCounter  atomic.Uint64
	duplicateCounter atomic.Uint64
	violationCounter atomic.Uint64
	fault429Counter  atomic.Uint64
	fault5xxCounter  atomic.Uint64

	shuttingDown atomic.Bool
}

func newServer(listen string, fault429, fault5xx int) *server {
	s := &server{
		startTime:       time.Now(),
		listen:          listen,
		fault429Percent: fault429,
		fault5xxPercent: fault5xx,
	}
	for i := range s.shards {
		s.shards[i] = &shard{m: make(map[string]*entityState)}
	}

	s.registry = prometheus.NewRegistry()
	s.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	s.acceptedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "target_server_accepted_total",
		Help: "Events accepted in submission order, by entity bucket.",
	}, []string{"bucket"})
	s.duplicateTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "target_server_duplicate_total",
		Help: "Idempotent duplicate redeliveries (FR-017), by bucket.",
	}, []string{"bucket"})
	s.violationTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "target_server_ordering_violation_total",
		Help: "Out-of-order arrivals (FR-016), by bucket.",
	}, []string{"bucket"})
	s.faultInjected = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "target_server_fault_injected_total",
		Help: "Requests answered with an injected fault, by status.",
	}, []string{"status"})
	s.requestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "target_server_request_duration_seconds",
		Help:    "Ingest handler latency.",
		Buckets: prometheus.DefBuckets,
	})
	s.faultPercentGge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "target_server_fault_percent",
		Help: "Currently configured fault rates.",
	}, []string{"kind"})
	s.registry.MustRegister(
		s.acceptedTotal, s.duplicateTotal, s.violationTotal,
		s.faultInjected, s.requestDuration, s.faultPercentGge,
	)
	s.faultPercentGge.WithLabelValues("429").Set(float64(fault429))
	s.faultPercentGge.WithLabelValues("5xx").Set(float64(fault5xx))
	return s
}

func bucketLabel(entityID string) string {
	return strconv.FormatUint(uint64(murmur3.Sum32([]byte(entityID))%numBuckets), 10)
}

func (s *server) shardFor(entityID string) *shard {
	idx := int(murmur3.Sum32([]byte(entityID)) % numBuckets)
	return s.shards[idx]
}

func (s *server) ingestHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		s.requestDuration.Observe(time.Since(start).Seconds())
		s.requestsServed.Add(1)
	}()

	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if ct := r.Header.Get("Content-Type"); ct == "" || !strings.Contains(ct, "application/json") {
		writeJSONError(w, http.StatusBadRequest, "Content-Type must be application/json")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	if len(body) > maxBodyBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "body exceeds 64 KiB")
		return
	}

	entityID := r.Header.Get("X-Entity-ID")
	if entityID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing X-Entity-ID")
		return
	}
	seqStr := r.Header.Get("X-Entity-Seq")
	if seqStr == "" {
		writeJSONError(w, http.StatusBadRequest, "missing X-Entity-Seq")
		return
	}
	recv, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil || recv == 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid X-Entity-Seq")
		return
	}
	taskID := r.Header.Get("X-Task-ID")

	// Fault injection: roll uniform random; short-circuit before any state mutation.
	roll := rand.Intn(100)
	if roll < s.fault429Percent {
		s.fault429Counter.Add(1)
		s.faultInjected.WithLabelValues("429").Inc()
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"status":         "throttled",
			"retry_after_ms": 250,
		})
		return
	}
	if roll < s.fault429Percent+s.fault5xxPercent {
		s.fault5xxCounter.Add(1)
		s.faultInjected.WithLabelValues("503").Inc()
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":         "throttled",
			"retry_after_ms": 250,
		})
		return
	}

	bucket := bucketLabel(entityID)
	sh := s.shardFor(entityID)
	sh.mu.Lock()
	st, ok := sh.m[entityID]
	if !ok {
		st = &entityState{}
		sh.m[entityID] = st
	}
	last := st.LastAcceptedSeq
	now := time.Now().UTC()

	switch {
	case recv > last:
		st.LastAcceptedSeq = recv
		st.Accepted++
		st.LastAcceptedAt = now
		sh.mu.Unlock()
		s.acceptedCounter.Add(1)
		s.acceptedTotal.WithLabelValues(bucket).Inc()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        "accepted",
			"entity_id":     entityID,
			"entity_seq":    recv,
			"last_accepted": recv,
		})

	case recv == last:
		sh.mu.Unlock()
		s.duplicateCounter.Add(1)
		s.duplicateTotal.WithLabelValues(bucket).Inc()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        "duplicate",
			"entity_id":     entityID,
			"entity_seq":    recv,
			"last_accepted": last,
		})

	default: // recv < last → rewind violation
		sh.mu.Unlock()
		s.violationCounter.Add(1)
		s.violationTotal.WithLabelValues(bucket).Inc()
		slog.Warn("ordering violation",
			"entity_id", entityID,
			"last_accepted_seq", last,
			"received_seq", recv,
			"task_id", taskID,
			"kind", "rewind",
		)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        "violation",
			"entity_id":     entityID,
			"entity_seq":    recv,
			"last_accepted": last,
		})
	}
}

func (s *server) healthzHandler(w http.ResponseWriter, _ *http.Request) {
	if s.shuttingDown.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "shutting_down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *server) stateHandler(w http.ResponseWriter, r *http.Request) {
	top := 50
	if t := r.URL.Query().Get("top"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			if n > 1000 {
				n = 1000
			}
			top = n
		}
	}

	type entityRow struct {
		EntityID        string    `json:"entity_id"`
		LastAcceptedSeq uint64    `json:"last_accepted_seq"`
		Accepted        uint64    `json:"accepted"`
		LastAcceptedAt  time.Time `json:"last_accepted_at"`
	}

	rows := make([]entityRow, 0)
	for _, sh := range s.shards {
		sh.mu.Lock()
		for id, st := range sh.m {
			rows = append(rows, entityRow{
				EntityID:        id,
				LastAcceptedSeq: st.LastAcceptedSeq,
				Accepted:        st.Accepted,
				LastAcceptedAt:  st.LastAcceptedAt,
			})
		}
		sh.mu.Unlock()
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].LastAcceptedAt.After(rows[j].LastAcceptedAt)
	})
	if len(rows) > top {
		rows = rows[:top]
	}

	resp := map[string]any{
		"config": map[string]any{
			"fault_429_percent": s.fault429Percent,
			"fault_5xx_percent": s.fault5xxPercent,
			"listen":            s.listen,
		},
		"totals": map[string]any{
			"accepted_total":  s.acceptedCounter.Load(),
			"duplicate_total": s.duplicateCounter.Load(),
			"violation_total": s.violationCounter.Load(),
			"fault_429_total": s.fault429Counter.Load(),
			"fault_5xx_total": s.fault5xxCounter.Load(),
		},
		"top_entities": rows,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) uniqueEntities() int {
	n := 0
	for _, sh := range s.shards {
		sh.mu.Lock()
		n += len(sh.m)
		sh.mu.Unlock()
	}
	return n
}

func (s *server) printStats() {
	uptime := time.Since(s.startTime).Round(time.Second)
	hours := int(uptime.Hours())
	minutes := int(uptime.Minutes()) % 60
	seconds := int(uptime.Seconds()) % 60

	fmt.Printf("=== target server stats ===\n")
	fmt.Printf("uptime              :  %02dh %02dm %02ds\n", hours, minutes, seconds)
	fmt.Printf("fault_429_percent   :  %d\n", s.fault429Percent)
	fmt.Printf("fault_5xx_percent   :  %d\n", s.fault5xxPercent)
	fmt.Printf("accepted_total      :  %d\n", s.acceptedCounter.Load())
	fmt.Printf("duplicate_total     :  %d\n", s.duplicateCounter.Load())
	fmt.Printf("violation_total     :  %d\n", s.violationCounter.Load())
	fmt.Printf("fault_429_total     :  %d\n", s.fault429Counter.Load())
	fmt.Printf("fault_5xx_total     :  %d\n", s.fault5xxCounter.Load())
	fmt.Printf("unique_entities     :  %d\n", s.uniqueEntities())
	fmt.Printf("===========================\n")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	listenFlag := flag.String("listen", envOr("LISTEN_ADDR", ":8080"), "ingest HTTP listener (env LISTEN_ADDR)")
	metricsPortFlag := flag.String("metrics-port", envOr("METRICS_PORT", ":9091"), "observability HTTP listener (env METRICS_PORT)")
	fault429Flag := flag.Int("fault-429-percent", envIntOr("FAULT_429_PERCENT", 0), "0..100; injected 429 rate (env FAULT_429_PERCENT)")
	fault5xxFlag := flag.Int("fault-5xx-percent", envIntOr("FAULT_5XX_PERCENT", 0), "0..100; injected 503 rate (env FAULT_5XX_PERCENT)")
	flag.Parse()

	if *fault429Flag < 0 || *fault429Flag > 100 || *fault5xxFlag < 0 || *fault5xxFlag > 100 {
		slog.Error("fault percent must be in [0, 100]", "fault_429_percent", *fault429Flag, "fault_5xx_percent", *fault5xxFlag)
		os.Exit(1)
	}
	if *fault429Flag+*fault5xxFlag > 100 {
		slog.Error("fault_429_percent + fault_5xx_percent must be <= 100",
			"fault_429_percent", *fault429Flag,
			"fault_5xx_percent", *fault5xxFlag,
		)
		os.Exit(1)
	}

	// Normalise metrics-port: accept "9091" or ":9091".
	metricsAddr := *metricsPortFlag
	if len(metricsAddr) > 0 && metricsAddr[0] != ':' {
		metricsAddr = ":" + metricsAddr
	}

	srv := newServer(*listenFlag, *fault429Flag, *fault5xxFlag)

	slog.Info("target server starting",
		"listen", *listenFlag,
		"metrics_port", metricsAddr,
		"fault_429_percent", *fault429Flag,
		"fault_5xx_percent", *fault5xxFlag,
	)

	ingestMux := http.NewServeMux()
	ingestMux.HandleFunc("/", srv.ingestHandler)

	obsMux := http.NewServeMux()
	obsMux.HandleFunc("/healthz", srv.healthzHandler)
	obsMux.HandleFunc("/state", srv.stateHandler)
	obsMux.Handle("/metrics", promhttp.HandlerFor(srv.registry, promhttp.HandlerOpts{}))

	ingestSrv := &http.Server{Addr: *listenFlag, Handler: ingestMux, ReadHeaderTimeout: 5 * time.Second}
	obsSrv := &http.Server{Addr: metricsAddr, Handler: obsMux, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		if err := ingestSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("ingest server failed", "err", err)
			stop()
		}
	}()
	go func() {
		if err := obsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("observability server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("target server shutting down")
	srv.shuttingDown.Store(true)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = ingestSrv.Shutdown(shutdownCtx)
	_ = obsSrv.Shutdown(shutdownCtx)

	srv.printStats()
	slog.Info("target server stopped")
}
