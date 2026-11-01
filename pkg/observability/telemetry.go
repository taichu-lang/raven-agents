package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func NewTracerProvider(ctx context.Context) (*trace.TracerProvider, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint("localhost:6006"),
		otlptracehttp.WithURLPath("/v1/traces"),
		otlptracehttp.WithInsecure(),
		// otlptracehttp.WithHeaders(map[string]string{
		// 	"Authorization": "Bearer xx",
		// }),
	}
	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	res, _ := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("agent-name"),
	))
	tp := trace.NewTracerProvider(trace.WithBatcher(exporter), trace.WithResource(res))

	// Set as global.
	otel.SetTracerProvider(tp)

	return tp, nil
}
