package api

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

type logCaptureHandler struct {
	records chan slog.Record
}

func (h *logCaptureHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *logCaptureHandler) Handle(_ context.Context, rec slog.Record) error {
	select {
	case h.records <- rec.Clone():
	default:
	}
	return nil
}

func (h *logCaptureHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *logCaptureHandler) WithGroup(string) slog.Handler {
	return h
}

func TestRunWebhookAsyncRecoversPanicAndLogsSafely(t *testing.T) {
	capture := &logCaptureHandler{records: make(chan slog.Record, 1)}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(prev)
	})

	s := &Server{}
	s.runWebhookAsync(context.Background(), "webhook panic test", []any{123, "bad", "repo_id", int64(55), "dangling"}, func(context.Context) error {
		panic("boom")
	})

	select {
	case rec := <-capture.records:
		if rec.Message != "async task panic" {
			t.Fatalf("expected async panic log, got %q", rec.Message)
		}
		attrs := map[string]any{}
		rec.Attrs(func(attr slog.Attr) bool {
			attrs[attr.Key] = attr.Value.Any()
			return true
		})
		if got := attrs["operation"]; got != "webhook panic test" {
			t.Fatalf("expected operation attr, got %v", got)
		}
		if got := attrs["attr_0"]; got != "bad" {
			t.Fatalf("expected malformed key attr to be normalized, got %v", got)
		}
		if got := attrs["repo_id"]; got != int64(55) {
			t.Fatalf("expected repo_id attr, got %v", got)
		}
		if got := attrs["dangling"]; got != "(missing)" {
			t.Fatalf("expected dangling attr to be marked missing, got %v", got)
		}
		if _, ok := attrs["stack"]; !ok {
			t.Fatal("expected panic stack attr")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async panic log")
	}
}

func TestRunWebhookAsyncDetachedContextIgnoresParentCancel(t *testing.T) {
	s := &Server{}
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	s.runWebhookAsync(parentCtx, "webhook detached context", nil, func(ctx context.Context) error {
		done <- ctx.Err()
		return nil
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected detached async context, got err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async task execution")
	}
}
