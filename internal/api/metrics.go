package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	metricsNamespace = "gothub"
	metricsSubsystem = "http"
)

type httpMetrics struct {
	requestTotal    *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	requestErrors   *prometheus.CounterVec
}

var (
	defaultHTTPMetricsOnce sync.Once
	defaultHTTPMetricsInst *httpMetrics
)

func getDefaultHTTPMetrics() *httpMetrics {
	defaultHTTPMetricsOnce.Do(func() {
		defaultHTTPMetricsInst = newHTTPMetrics(prometheus.DefaultRegisterer)
	})
	return defaultHTTPMetricsInst
}

func newHTTPMetrics(reg prometheus.Registerer) *httpMetrics {
	m := &httpMetrics{
		requestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "requests_total",
			Help:      "Total number of HTTP requests handled.",
		}, []string{"method", "route", "status_class"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route", "status_class"}),
		requestErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "errors_total",
			Help:      "Total number of HTTP requests with status >= 400.",
		}, []string{"method", "route", "status_code"}),
	}
	if reg != nil {
		reg.MustRegister(m.requestTotal, m.requestDuration, m.requestErrors)
	}
	return m
}

func requestMetricsMiddleware(metrics *httpMetrics, next http.Handler) http.Handler {
	if metrics == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Avoid recursive scrape accounting.
		if r.URL != nil && r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := requestRouteLabel(r)
		statusClass := httpStatusClass(rec.status)

		metrics.requestTotal.WithLabelValues(r.Method, route, statusClass).Inc()
		metrics.requestDuration.WithLabelValues(r.Method, route, statusClass).Observe(time.Since(start).Seconds())
		if rec.status >= http.StatusBadRequest {
			metrics.requestErrors.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
		}
	})
}

func metricsHandler(gatherer prometheus.Gatherer) http.Handler {
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

func requestRouteLabel(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "unknown"
	}

	if pattern := normalizeRoutePattern(r.Pattern); pattern != "" {
		return pattern
	}

	path := r.URL.Path
	switch {
	case path == "/healthz":
		return "/healthz"
	case path == "/metrics":
		return "/metrics"
	case strings.HasPrefix(path, "/api/v1/"):
		return "/api/v1/*"
	case strings.HasPrefix(path, "/git/"):
		return "/git/*"
	case strings.HasPrefix(path, "/got/"):
		return "/got/*"
	default:
		return "other"
	}
}

func normalizeRoutePattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	if _, route, ok := strings.Cut(pattern, " "); ok {
		return strings.TrimSpace(route)
	}
	return pattern
}

func httpStatusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}
