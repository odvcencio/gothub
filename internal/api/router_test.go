package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestChainMiddlewarePreservesOrder(t *testing.T) {
	sequence := make([]string, 0, 5)
	wrap := func(name string) middlewareFunc {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sequence = append(sequence, "before:"+name)
				next.ServeHTTP(w, r)
				sequence = append(sequence, "after:"+name)
			})
		}
	}

	handler := chainMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sequence = append(sequence, "handler")
			w.WriteHeader(http.StatusNoContent)
		}),
		wrap("outer"),
		wrap("inner"),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}

	want := []string{
		"before:outer",
		"before:inner",
		"handler",
		"after:inner",
		"after:outer",
	}
	if !reflect.DeepEqual(sequence, want) {
		t.Fatalf("unexpected middleware order: got %v want %v", sequence, want)
	}
}

func TestChainMiddlewareBuildsOnceAndReusesWrappedHandlers(t *testing.T) {
	builds := map[string]int{}
	calls := map[string]int{}
	wrap := func(name string) middlewareFunc {
		return func(next http.Handler) http.Handler {
			builds[name]++
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls[name]++
				next.ServeHTTP(w, r)
			})
		}
	}

	handler := chainMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls["handler"]++
			w.WriteHeader(http.StatusNoContent)
		}),
		wrap("tracing"),
		wrap("metrics"),
		wrap("logging"),
	)

	if got := builds["tracing"]; got != 1 {
		t.Fatalf("expected tracing middleware to be built once, got %d", got)
	}
	if got := builds["metrics"]; got != 1 {
		t.Fatalf("expected metrics middleware to be built once, got %d", got)
	}
	if got := builds["logging"]; got != 1 {
		t.Fatalf("expected logging middleware to be built once, got %d", got)
	}

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("request %d: expected status %d, got %d", i, http.StatusNoContent, rec.Code)
		}
	}

	if got := builds["tracing"]; got != 1 {
		t.Fatalf("expected tracing middleware build count to remain 1, got %d", got)
	}
	if got := builds["metrics"]; got != 1 {
		t.Fatalf("expected metrics middleware build count to remain 1, got %d", got)
	}
	if got := builds["logging"]; got != 1 {
		t.Fatalf("expected logging middleware build count to remain 1, got %d", got)
	}

	if got := calls["tracing"]; got != 3 {
		t.Fatalf("expected tracing middleware to run 3 times, got %d", got)
	}
	if got := calls["metrics"]; got != 3 {
		t.Fatalf("expected metrics middleware to run 3 times, got %d", got)
	}
	if got := calls["logging"]; got != 3 {
		t.Fatalf("expected logging middleware to run 3 times, got %d", got)
	}
	if got := calls["handler"]; got != 3 {
		t.Fatalf("expected base handler to run 3 times, got %d", got)
	}
}
