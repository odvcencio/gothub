package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func initTracing(ctx context.Context) (func(context.Context) error, error) {
	endpoint := strings.TrimSpace(os.Getenv("GOTHUB_OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracehttp.Option{}
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		opts = append(opts, otlptracehttp.WithEndpoint(u.Host))
		if strings.EqualFold(u.Scheme, "http") {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
	} else {
		opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
	}

	if envBool("GOTHUB_OTEL_EXPORTER_OTLP_INSECURE") {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}

	serviceName := strings.TrimSpace(os.Getenv("GOTHUB_OTEL_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = "gothub"
	}
	res := resource.NewWithAttributes("", attribute.String("service.name", serviceName))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func envBool(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
