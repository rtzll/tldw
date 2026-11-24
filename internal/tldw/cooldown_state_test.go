package tldw

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func TestSupersededProbeCannotClearCooldown(t *testing.T) {
	var c YouTubeCooldown
	c.finish(nil, fetchRateLimited, time.Second, trace.SpanContext{})
	c.window.until = time.Now().Add(-time.Second)
	probe, err := c.admit(t.Context(), &youtubeReadBudget{}, nil)
	if err != nil || probe == nil || c.phase != probing {
		t.Fatalf("probe=%v phase=%v err=%v", probe, c.phase, err)
	}
	// A concurrent ordinary read discovers a newer limit while the probe runs.
	c.finish(nil, fetchRateLimited, time.Minute, trace.SpanContext{})
	if c.phase != probing {
		t.Fatal("overlapping read released the in-flight probe")
	}
	c.finish(probe, fetchSucceeded, 0, trace.SpanContext{})
	if c.phase != cooling || time.Until(c.window.until) <= 0 {
		t.Fatal("superseded probe cleared cooldown")
	}
}
