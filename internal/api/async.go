package api

import (
	"context"
	"log/slog"
	"time"
)

const asyncTaskTimeout = 20 * time.Second

func (s *Server) runAsync(ctx context.Context, operation string, attrs []any, fn func(context.Context) error) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				fields := append([]any{"operation", operation, "panic", rec}, attrs...)
				slog.Error("async task panic", fields...)
			}
		}()

		taskCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), asyncTaskTimeout)
		defer cancel()

		if err := fn(taskCtx); err != nil {
			fields := append([]any{"operation", operation, "error", err}, attrs...)
			slog.Error("async task failed", fields...)
		}
	}()
}
