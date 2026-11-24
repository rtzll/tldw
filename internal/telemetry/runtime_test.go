package telemetry

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func TestPanicReachesCallerWithoutItsPayloadEnteringTraces(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var out bytes.Buffer
	p, err := NewProvider(t.Context(), "test", &out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Shutdown(p) }()
	const secret = "private request body"
	func() {
		defer func() {
			if got := recover(); got != secret {
				t.Fatalf("unexpected panic: %v", got)
			}
		}()
		ctx, root := p.Tracer(Scope).Start(t.Context(), "request")
		defer EndSpan(root)
		_, child := Start(ctx, "sdk operation")
		defer child.End()
		panic(secret)
	}()
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), `"error.type":"panic"`) {
		t.Fatalf("unexpected trace: %s", out.String())
	}
}

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
	type record struct {
		Name    string           `json:"msg"`
		Event   string           `json:"event"`
		Trace   string           `json:"trace_id"`
		ID      string           `json:"span_id"`
		Parent  string           `json:"parent_span_id"`
		Outcome string           `json:"outcome"`
		Links   []map[string]any `json:"links"`
		Events  []map[string]any `json:"events"`
	}
	ended := map[string]record{}
	starts := 0
	for dec.More() {
		var r record
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		switch r.Event {
		case "span.start":
			starts++
		case "span.end":
			ended[r.Name] = r
		default:
			t.Fatalf("unexpected record: %+v", r)
		}
	}
	if starts != 3 || len(ended) != 3 {
		t.Fatalf("starts=%d ended=%d", starts, len(ended))
	}
	a, b, c := ended["attempt"], ended["first"], ended["second"]
	if a.Event != "span.end" || b.Event != "span.end" || c.Event != "span.end" || a.Parent != b.ID || a.Trace != b.Trace || c.Trace == a.Trace || a.Outcome != "error" || len(c.Links) != 1 || c.Links[0]["span_id"] != a.ID || len(c.Events) != 1 {
		t.Fatalf("unexpected span relationships: %+v %+v %+v", a, b, c)
	}
}
