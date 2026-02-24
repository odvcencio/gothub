package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRequestTracingMiddlewareCreatesRequestSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider()
	tp.RegisterSpanProcessor(recorder)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		_ = tp.Shutdown(context.Background())
	})

	handler := requestTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos/acme/demo", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.Code)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected one span, got %d", len(spans))
	}
	span := spans[0]
	if got, want := span.Name(), "POST /api/v1/*"; got != want {
		t.Fatalf("expected span name %q, got %q", want, got)
	}
	if span.Status().Code != codes.Ok {
		t.Fatalf("expected span status Ok, got %v", span.Status().Code)
	}
	if !containsStringAttribute(span.Attributes(), "http.method", http.MethodPost) {
		t.Fatal("expected span attribute http.method=POST")
	}
	if !containsStringAttribute(span.Attributes(), "http.route", "/api/v1/*") {
		t.Fatal("expected span attribute http.route=/api/v1/*")
	}
	if !containsIntAttribute(span.Attributes(), "http.status_code", http.StatusCreated) {
		t.Fatal("expected span attribute http.status_code=201")
	}
}

func TestRequestTracingMiddlewareSkipsPprofEndpoint(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider()
	tp.RegisterSpanProcessor(recorder)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		_ = tp.Shutdown(context.Background())
	})

	handler := requestTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("expected no spans for /debug/pprof/, got %d", got)
	}
}

func containsStringAttribute(attrs []attribute.KeyValue, key, value string) bool {
	for _, attr := range attrs {
		if string(attr.Key) == key && attr.Value.Type() == attribute.STRING && attr.Value.AsString() == value {
			return true
		}
	}
	return false
}

func containsIntAttribute(attrs []attribute.KeyValue, key string, value int) bool {
	for _, attr := range attrs {
		if string(attr.Key) == key && attr.Value.Type() == attribute.INT64 && attr.Value.AsInt64() == int64(value) {
			return true
		}
	}
	return false
}
