package telemetry

import (
	"context"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// The local recorder writes immediately, including when a remote exporter is offline.
type localRecorder struct{ log *slog.Logger }

func fields(attrs []attribute.KeyValue) map[string]any {
	result := make(map[string]any, len(attrs))
	for _, kv := range attrs {
		result[string(kv.Key)] = kv.Value.AsInterface()
	}
	return result
}

func recordFields(s sdktrace.ReadOnlySpan, event string) []slog.Attr {
	sc := s.SpanContext()
	attrs := []slog.Attr{
		slog.String("event", event),
		slog.String("trace_id", sc.TraceID().String()),
		slog.String("span_id", sc.SpanID().String()),
		slog.Time("start", s.StartTime()),
		slog.Any("attributes", fields(s.Attributes())),
	}
	if parent := s.Parent(); parent.IsValid() {
		attrs = append(attrs, slog.String("parent_span_id", parent.SpanID().String()))
	}
	return attrs
}

// Persist ingress and outbound download starts even if the process never
// reaches End. Cheap internal decisions only need a completion record.
func (e *localRecorder) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	if s.Parent().IsValid() && s.SpanKind() != trace.SpanKindServer && !strings.HasPrefix(s.Name(), "yt-dlp.") {
		return
	}
	e.log.LogAttrs(ctx, slog.LevelInfo, s.Name(), recordFields(s, "span.start")...)
}

func (e *localRecorder) OnEnd(s sdktrace.ReadOnlySpan) {
	attrs := recordFields(s, "span.end")
	attrs = append(attrs, slog.Float64("duration_ms", float64(s.EndTime().Sub(s.StartTime()).Microseconds())/1000))
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
	e.log.LogAttrs(context.Background(), level, s.Name(), attrs...)
}

func (*localRecorder) Shutdown(context.Context) error   { return nil }
func (*localRecorder) ForceFlush(context.Context) error { return nil }
