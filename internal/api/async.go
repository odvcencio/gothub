package api

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strconv"
	"time"
)

const asyncTaskTimeout = 20 * time.Second

func (s *Server) runAsync(ctx context.Context, operation string, attrs []any, fn func(context.Context) error) {
	safeAttrs := append([]any(nil), attrs...)
	parentCtx := ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logAttrs := asyncLogAttrs(operation, safeAttrs, "panic", rec)
				logAttrs = append(logAttrs, slog.String("stack", string(debug.Stack())))
				slog.LogAttrs(context.Background(), slog.LevelError, "async task panic", logAttrs...)
			}
		}()

		taskCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), asyncTaskTimeout)
		defer cancel()

		if err := fn(taskCtx); err != nil {
			slog.LogAttrs(taskCtx, slog.LevelError, "async task failed", asyncLogAttrs(operation, safeAttrs, "error", err)...)
		}
	}()
}

func (s *Server) runWebhookAsync(ctx context.Context, operation string, attrs []any, fn func(context.Context) error) {
	s.runAsync(ctx, operation, attrs, fn)
}

func asyncLogAttrs(operation string, attrs []any, key string, value any) []slog.Attr {
	logAttrs := make([]slog.Attr, 0, 2+(len(attrs)+1)/2)
	logAttrs = append(logAttrs, slog.String("operation", operation), slog.Any(key, value))
	for i := 0; i < len(attrs); i += 2 {
		attrKey := "attr_" + strconv.Itoa(i/2)
		if k, ok := attrs[i].(string); ok && k != "" {
			attrKey = k
		}
		attrValue := any("(missing)")
		if i+1 < len(attrs) {
			attrValue = attrs[i+1]
		}
		logAttrs = append(logAttrs, slog.Any(attrKey, attrValue))
	}
	return logAttrs
}
