package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var exportMetrics = struct {
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestsInFlight prometheus.Gauge
}{
	httpRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "export",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests processed by the export service.",
	}, []string{"method", "path", "status"}),

	httpRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "export",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency distribution.",
		Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"method", "path"}),

	httpRequestsInFlight: promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "export",
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
		exportMetrics.httpRequestsInFlight.Inc()
		defer exportMetrics.httpRequestsInFlight.Dec()

		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(rw.statusCode)
		path := normalizePath(r.URL.Path)

		exportMetrics.httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		exportMetrics.httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// normalizePath collapses dynamic segments to keep label cardinality bounded.
func normalizePath(p string) string {
	switch {
	case p == "/health" || p == "/metrics" || p == "/export/v1/openapi":
		return p
	case strings.HasPrefix(p, "/export/v1"):
		return "/export/v1"
	}
	return "/unknown"
}
