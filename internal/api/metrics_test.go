package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRequestMetricsMiddlewareRecordsRequestMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := newHTTPMetrics(reg)

	handler := requestMetricsMiddleware(metrics, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/demo", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.Code)
	}

	if got := testutil.ToFloat64(metrics.requestTotal.WithLabelValues(http.MethodGet, "/api/v1/*", "5xx")); got != 1 {
		t.Fatalf("expected request counter 1, got %f", got)
	}
	if got := testutil.ToFloat64(metrics.requestErrors.WithLabelValues(http.MethodGet, "/api/v1/*", "500")); got != 1 {
		t.Fatalf("expected error counter 1, got %f", got)
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	foundDuration := false
	for _, mf := range mfs {
		if mf.GetName() != "gothub_http_request_duration_seconds" {
			continue
		}
		foundDuration = true
		if len(mf.Metric) != 1 {
			t.Fatalf("expected one latency series, got %d", len(mf.Metric))
		}
		if got := mf.Metric[0].GetHistogram().GetSampleCount(); got != 1 {
			t.Fatalf("expected one latency sample, got %d", got)
		}
	}
	if !foundDuration {
		t.Fatal("did not find latency histogram")
	}
}

func TestRequestMetricsMiddlewareSkipsMetricsEndpoint(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := newHTTPMetrics(reg)

	handler := requestMetricsMiddleware(metrics, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if got := testutil.CollectAndCount(metrics.requestTotal); got != 0 {
		t.Fatalf("expected no request samples for /metrics, got %d", got)
	}
}

func TestMetricsHandlerExposesMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := newHTTPMetrics(reg)

	observed := requestMetricsMiddleware(metrics, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	observed.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp := httptest.NewRecorder()
	metricsHandler(reg).ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, name := range []string{
		"gothub_http_requests_total",
		"gothub_http_request_duration_seconds",
		"gothub_http_errors_total",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("expected scrape output to contain %q", name)
		}
	}
}
