// Package observability wires up OpenTelemetry tracing for the server:
// one TracerProvider registered as the process-wide global (via
// otel.SetTracerProvider), shared by the HTTP layer (otelhttp) and ADK Go
// 2.0's own instrumentation. ADK is OTel-native and reads spans through
// otel.Tracer(...) internally — registering the global provider here is
// enough to join it to the same trace, with no ADK import needed (and
// spec-10's import-confinement rule keeps every google.golang.org/adk
// import inside internal/orchestrator/adk, this package included by
// design). A single Bundle run's trace therefore spans the full chain:
// HTTP request -> orchestration -> ADK agent step -> model call. See
// spec-19.
package observability

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Setup builds a TracerProvider and registers it as the global OTel
// provider. Honors the standard OTEL_EXPORTER_OTLP_ENDPOINT env var. When
// unset (the common case in local dev and CI, where no collector is
// running), tracing stays a documented no-op: spans are created and
// dropped locally rather than failing requests or blocking on a collector
// that will never answer.
func Setup(ctx context.Context, serviceName, serviceVersion string) (shutdown func(context.Context) error, err error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("observability: build resource: %w", err)
	}

	tpOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}

	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("observability: build OTLP exporter: %w", err)
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exporter))
	}
	// No endpoint configured: the TracerProvider still has no exporter
	// attached, so otel.Tracer(...) calls throughout the codebase (and
	// ADK's own spans) are valid, low-overhead no-ops instead of
	// nil-provider panics.

	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

// TraceIDFromContext returns the hex-encoded trace ID of the span active in
// ctx, or "" if there is none (no otelhttp span, e.g. in unit tests that
// call a handler directly without the middleware chain).
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
