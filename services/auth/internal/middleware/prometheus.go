package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// authMetrics holds all Prometheus metrics for the auth service.
var authMetrics = struct {
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestsInFlight prometheus.Gauge
	dbQueryDuration      *prometheus.HistogramVec
}{
	httpRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "auth",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests processed by the auth service.",
	}, []string{"method", "path", "status"}),

	httpRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "auth",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency distribution.",
		Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"method", "path"}),

	httpRequestsInFlight: promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "auth",
		Name:      "http_requests_in_flight",
		Help:      "Current number of HTTP requests being processed.",
	}),

	dbQueryDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "auth",
		Name:      "db_query_duration_seconds",
		Help:      "Database query latency distribution.",
		Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	}, []string{"operation"}),
}

// responseWriter wraps http.ResponseWriter to capture the status code.
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

// PrometheusMiddleware instruments every HTTP request with Prometheus metrics.
// It records:
//   - auth_http_requests_total       (counter, labelled by method/path/status)
//   - auth_http_request_duration_seconds (histogram, labelled by method/path)
//   - auth_http_requests_in_flight   (gauge)
func PrometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip /metrics itself to avoid self-scrape noise.
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		authMetrics.httpRequestsInFlight.Inc()
		defer authMetrics.httpRequestsInFlight.Dec()

		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(rw.statusCode)
		path := normalizePath(r.URL.Path)

		authMetrics.httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		authMetrics.httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// normalizePath replaces dynamic path segments with placeholders so that
// high-cardinality paths (e.g. /auth/users/uuid) don't create unbounded label sets.
func normalizePath(p string) string {
	known := map[string]bool{
		"/health":        true,
		"/auth/register": true,
		"/auth/login":    true,
		"/auth/refresh":  true,
		"/auth/me":       true,
		"/auth/password": true,
		"/auth/sessions": true,
		"/auth/account":  true,
		"/auth/logout":   true,
		"/auth/users":    true,
		"/auth/roles":    true,
		"/metrics":       true,
	}
	if known[p] {
		return p
	}
	// Collapse dynamic user IDs: /auth/users/{id}
	if strings.HasPrefix(p, "/auth/users/") {
		return "/auth/users/{id}"
	}
	return "/unknown"
}
