package telemetry

import (
	"bytes"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func TestLocalSpansCorrelateAcrossRequests(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var out bytes.Buffer
	p, err := NewProvider(t.Context(), "test", &out)
	if err != nil {
		t.Fatal(err)
	}
	tracer := p.Tracer(Scope)
	ctx, origin := tracer.Start(t.Context(), "first")
	_, attempt := Start(ctx, "attempt")
	attempt.SetAttributes(attribute.String("track", "en-orig"))
	attempt.SetStatus(codes.Error, "download")
	attempt.End()
	origin.End()
	_, waiter := tracer.Start(t.Context(), "second", trace.WithLinks(trace.Link{SpanContext: attempt.SpanContext()}))
	waiter.AddEvent("cooldown")
	waiter.End()
	if err := Shutdown(p); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	var a, b, c struct {
		Trace   string           `json:"trace_id"`
		ID      string           `json:"span_id"`
		Parent  string           `json:"parent_span_id"`
		Outcome string           `json:"outcome"`
		Links   []map[string]any `json:"links"`
		Events  []map[string]any `json:"events"`
	}
	for _, v := range []any{&a, &b, &c} {
		if err := dec.Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	if a.Parent != b.ID || a.Trace != b.Trace || c.Trace == a.Trace || a.Outcome != "error" || len(c.Links) != 1 || c.Links[0]["span_id"] != a.ID || len(c.Events) != 1 {
		t.Fatalf("unexpected span relationships: %+v %+v %+v", a, b, c)
	}
}
