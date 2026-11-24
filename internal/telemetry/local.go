package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// The local file is usable without a collector. It contains one JSON object
// per completed span. Only explicitly instrumented fields are recorded.
type localExporter struct{ log *slog.Logger }

func fields(attrs []attribute.KeyValue) map[string]any {
	result := make(map[string]any, len(attrs))
	for _, kv := range attrs {
		result[string(kv.Key)] = kv.Value.AsInterface()
	}
	return result
}

func (e *localExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	for _, s := range spans {
		sc := s.SpanContext()
		attrs := []slog.Attr{
			slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()),
			slog.Time("start", s.StartTime()), slog.Float64("duration_ms", float64(s.EndTime().Sub(s.StartTime()).Microseconds())/1000),
			slog.Any("attributes", fields(s.Attributes())),
		}
		if s.Parent().IsValid() {
			attrs = append(attrs, slog.String("parent_span_id", s.Parent().SpanID().String()))
		}
		for _, link := range s.Links() {
			// One list permits multiple causes without duplicate JSON keys.
			_ = link
		}
		if links := s.Links(); len(links) > 0 {
			values := make([]map[string]any, 0, len(links))
			for _, l := range links {
				values = append(values, map[string]any{"trace_id": l.SpanContext.TraceID().String(), "span_id": l.SpanContext.SpanID().String(), "attributes": fields(l.Attributes)})
			}
			attrs = append(attrs, slog.Any("links", values))
		}
		if events := s.Events(); len(events) > 0 {
			values := make([]map[string]any, 0, len(events))
			for _, ev := range events {
				values = append(values, map[string]any{"name": ev.Name, "time": ev.Time, "attributes": fields(ev.Attributes)})
			}
			attrs = append(attrs, slog.Any("events", values))
		}
		level := slog.LevelInfo
		outcome := "ok"
		if s.Status().Code == codes.Error {
			level = slog.LevelError
			outcome = "error"
		}
		attrs = append(attrs, slog.String("outcome", outcome))
		e.log.LogAttrs(ctx, level, s.Name(), attrs...)
	}
	return nil
}

func (*localExporter) Shutdown(context.Context) error { return nil }
