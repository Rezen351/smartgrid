package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var moduleMetrics = struct {
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestsInFlight prometheus.Gauge
}{
	httpRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "module",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests processed by the module service.",
	}, []string{"method", "path", "status"}),

	httpRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "module",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency distribution.",
		Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"method", "path"}),

	httpRequestsInFlight: promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "module",
		Name:      "http_requests_in_flight",
		Help:      "Current number of HTTP requests being processed.",
	}),
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

// PrometheusMiddleware instruments every HTTP request.
func PrometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		moduleMetrics.httpRequestsInFlight.Inc()
		defer moduleMetrics.httpRequestsInFlight.Dec()

		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(rw.statusCode)
		path := normalizePath(r.URL.Path)

		moduleMetrics.httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		moduleMetrics.httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// normalizePath collapses dynamic segments to keep label cardinality bounded.
func normalizePath(p string) string {
	known := map[string]bool{
		"/health":           true,
		"/metrics":          true,
		"/modules":          true,
		"/nodes":            true,
		"/nodes/discovered": true,
	}
	if known[p] {
		return p
	}
	switch {
	case strings.HasPrefix(p, "/modules/"):
		return "/modules/{id}"
	case strings.HasSuffix(p, "/pair"):
		return "/nodes/{node_id}/pair"
	case strings.HasSuffix(p, "/unpair"):
		return "/nodes/{node_id}/unpair"
	case strings.HasPrefix(p, "/nodes/"):
		return "/nodes/{node_id}"
	}
	return "/unknown"
}
