package observability

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// Providers holds SDK providers for graceful shutdown.
type Providers struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
}

// SetupNoop installs no-op global providers (safe default for Cursor hot path).
func SetupNoop() *Providers {
	tp := sdktrace.NewTracerProvider()
	mp := sdkmetric.NewMeterProvider()
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	return &Providers{TracerProvider: tp, MeterProvider: mp}
}

func newResource() (*resource.Resource, error) {
	return resource.Merge(
		resource.Default(),
		// Schemaless: resource.Default() carries its own schema URL (SDK-defined),
		// and pinning semconv here conflicts with it at Merge time.
		resource.NewSchemaless(
			semconv.ServiceName("self-improving-mcp"),
			semconv.ServiceVersion("0.1.0"),
		),
	)
}

// Tracer returns a named tracer from the global provider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// Meter returns a named meter from the global provider.
func Meter(name string) metric.Meter {
	return otel.Meter(name)
}
