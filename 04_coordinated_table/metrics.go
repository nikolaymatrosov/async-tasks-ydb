package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsHandler returns an http.Handler that serves Prometheus metrics from registry.
// Only /metrics is served; all other paths return 404.
func metricsHandler(registry *prometheus.Registry) http.Handler {
	inner := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
}
