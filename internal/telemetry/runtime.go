// Package telemetry configures tracing at the application boundary.
// Instrumented operations inherit their provider from the active span.
package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const Scope = "github.com/rtzll/tldw"

// NewProvider records locally when a writer is supplied. Network export is
// opt-in. Both sinks consume the same ended spans, including their links.
func NewProvider(ctx context.Context, version string, local io.Writer) (*sdktrace.TracerProvider, error) {
	export := ExportConfigured()
	sampler := sdktrace.NeverSample()
	if local != nil || export {
		sampler = sdktrace.AlwaysSample()
	}
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "tldw"), attribute.String("service.version", version))),
		sdktrace.WithSampler(sampler),
	}
	if local != nil {
		logger := slog.New(slog.NewJSONHandler(local, nil)).With("service.version", version)
		opts = append(opts, sdktrace.WithSpanProcessor(&localRecorder{logger}))
	}
	if export {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return sdktrace.NewTracerProvider(opts...), err
		}
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}
	return sdktrace.NewTracerProvider(opts...), nil
}

// Shutdown allows a canceled CLI invocation to flush, with a bounded wait.
func Shutdown(provider *sdktrace.TracerProvider) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return provider.Shutdown(ctx)
}

func Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return trace.SpanFromContext(ctx).TracerProvider().Tracer(Scope).Start(ctx, name, opts...)
}

func ExportConfigured() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}
