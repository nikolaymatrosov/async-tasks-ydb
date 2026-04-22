package main

import (
	"fmt"
	"net/http"
)

// metricsHandler returns an http.HandlerFunc that serves Prometheus text format
// metrics for the given Stats and workerID. Only /metrics is served; all other
// paths return 404.
func metricsHandler(s *Stats, workerID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}

		processed := s.tasksProcessed.Load()
		locked := s.tasksLocked.Load()
		errors := s.tasksErrors.Load()
		partitions := s.partitionsOwned.Load()

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# HELP coordinator_tasks_processed_total Cumulative tasks marked completed by this worker\n")
		fmt.Fprintf(w, "# TYPE coordinator_tasks_processed_total counter\n")
		fmt.Fprintf(w, "coordinator_tasks_processed_total{worker_id=%q} %d\n", workerID, processed)

		fmt.Fprintf(w, "# HELP coordinator_tasks_locked_total Cumulative tasks locked (includes retries)\n")
		fmt.Fprintf(w, "# TYPE coordinator_tasks_locked_total counter\n")
		fmt.Fprintf(w, "coordinator_tasks_locked_total{worker_id=%q} %d\n", workerID, locked)

		fmt.Fprintf(w, "# HELP coordinator_tasks_errors_total Cumulative failed lock or complete operations\n")
		fmt.Fprintf(w, "# TYPE coordinator_tasks_errors_total counter\n")
		fmt.Fprintf(w, "coordinator_tasks_errors_total{worker_id=%q} %d\n", workerID, errors)

		fmt.Fprintf(w, "# HELP coordinator_partitions_owned Current number of partitions owned by this worker\n")
		fmt.Fprintf(w, "# TYPE coordinator_partitions_owned gauge\n")
		fmt.Fprintf(w, "coordinator_partitions_owned{worker_id=%q} %d\n", workerID, partitions)

		fmt.Fprintf(w, "# HELP coordinator_up 1 if the worker process is running, 0 otherwise\n")
		fmt.Fprintf(w, "# TYPE coordinator_up gauge\n")
		fmt.Fprintf(w, "coordinator_up{worker_id=%q} 1\n", workerID)
	}
}
