package api

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const apiTracerName = "github.com/odvcencio/gothub/internal/api"

func requestTracingMiddleware(next http.Handler) http.Handler {
	tracer := otel.Tracer(apiTracerName)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldSkipRequestInstrumentation(r) {
			next.ServeHTTP(w, r)
			return
		}

		route := requestRouteLabel(r)
		spanName := fmt.Sprintf("%s %s", r.Method, route)

		ctx, span := tracer.Start(r.Context(), spanName, trace.WithSpanKind(trace.SpanKindServer))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			span.SetAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", route),
				attribute.Int("http.status_code", rec.status),
			)
			if rec.status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(rec.status))
			} else {
				span.SetStatus(codes.Ok, "")
			}
			span.End()
		}()

		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}
