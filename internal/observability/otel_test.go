package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
)

func TestSetup_RegistersGlobalTracerProvider_AndShutsDown(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, "test-service", "0.0.1")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	tracer := otel.Tracer("test")
	_, span := tracer.Start(ctx, "test-span")
	defer span.End()

	if !span.SpanContext().HasTraceID() {
		t.Fatal("expected a real trace ID from a span created after Setup")
	}
}

func TestTraceIDFromContext_EmptyWithoutSpan(t *testing.T) {
	if id := TraceIDFromContext(context.Background()); id != "" {
		t.Fatalf("expected empty trace ID for a context with no span, got %q", id)
	}
}

func TestTraceIDFromContext_ReturnsSpanTraceID(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, "test-service", "0.0.1")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer func() { _ = shutdown(ctx) }()

	tracer := otel.Tracer("test")
	spanCtx, span := tracer.Start(ctx, "test-span")
	defer span.End()

	id := TraceIDFromContext(spanCtx)
	if id == "" {
		t.Fatal("expected a non-empty trace ID")
	}
	if id != span.SpanContext().TraceID().String() {
		t.Fatalf("TraceIDFromContext = %q, want %q", id, span.SpanContext().TraceID().String())
	}
}
