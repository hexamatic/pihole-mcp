//go:build !slim

// Package telemetry provides optional OpenTelemetry tracing.
// Tracing is only enabled when OTEL_EXPORTER_OTLP_ENDPOINT is set.
// Build with `-tags slim` to strip all OpenTelemetry code paths — this
// drops the otel/grpc/protobuf dependencies entirely, shrinking the
// binary by ~5-8MB at the cost of losing OTel support.
package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init creates a TracerProvider if OTEL_EXPORTER_OTLP_ENDPOINT is set.
// Returns a nil Provider if tracing is not configured (zero overhead).
func Init(serviceName, version string) (Provider, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return nil, nil
	}

	ctx := context.Background()

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}
