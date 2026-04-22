package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// Stats holds atomic counters shared between the worker and the display loop.
type Stats struct {
	workerID        string
	partitionsOwned atomic.Int64
	tasksProcessed  atomic.Int64
	tasksLocked     atomic.Int64
	tasksErrors     atomic.Int64
	startTime       time.Time
}

func newStats(workerID string) *Stats {
	return &Stats{
		workerID:  workerID,
		startTime: time.Now(),
	}
}

// display prints a periodic stats block every 5 seconds until ctx is done.
func (s *Stats) display(ctx context.Context) {
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
	partitions := s.partitionsOwned.Load()
	processed := s.tasksProcessed.Load()
	locked := s.tasksLocked.Load()
	errors := s.tasksErrors.Load()

	// Structured log for machine consumption.
	slog.Info("worker stats",
		"worker_id", s.workerID,
		"partitions_owned", partitions,
		"tasks_processed", processed,
		"tasks_locked", locked,
		"tasks_errors", errors,
		"uptime", uptime.String(),
	)

	// Plain-text stats block for human consumption.
	fmt.Printf("=== Worker %s Stats ===\n", s.workerID[:8])
	fmt.Printf("Partitions owned: %6d\n", partitions)
	fmt.Printf("Tasks processed:  %6d\n", processed)
	fmt.Printf("Tasks locked:     %6d\n", locked)
	fmt.Printf("Tasks errors:     %6d\n", errors)
	fmt.Printf("Uptime:           %6s\n", uptime)
	fmt.Printf("========================\n")
}
