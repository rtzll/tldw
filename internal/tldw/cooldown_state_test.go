package tldw

import (
	"go.opentelemetry.io/otel/trace"
	"testing"
	"time"
)

func TestSupersededProbeCannotClearCooldown(t *testing.T) {
	var c YouTubeCooldown
	c.finish(nil, nil, &YouTubeRateLimitError{RetryAfter: time.Second}, trace.SpanContext{})
	c.until = time.Now().Add(-time.Second)
	probe, err := c.admit(t.Context(), &youtubeReadBudget{}, nil)
	if err != nil || probe == nil || c.phase != probing {
		t.Fatalf("probe=%v phase=%v err=%v", probe, c.phase, err)
	}
	// A concurrent ordinary read discovers a newer limit while the probe runs.
	c.finish(nil, nil, &YouTubeRateLimitError{RetryAfter: time.Minute}, trace.SpanContext{})
	if c.phase != probing {
		t.Fatal("overlapping read released the in-flight probe")
	}
	c.finish(probe, nil, nil, trace.SpanContext{})
	if c.phase != cooling || time.Until(c.until) <= 0 {
		t.Fatal("superseded probe cleared cooldown")
	}
}
