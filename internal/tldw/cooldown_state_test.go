package tldw

import (
	"testing"
	"time"
)

func TestSupersededProbeCannotClearCooldown(t *testing.T) {
	var c YouTubeCooldown
	c.finish(nil, nil, &YouTubeRateLimitError{RetryAfter: time.Second})
	c.until = time.Now().Add(-time.Second)
	probe, err := c.admit(t.Context(), &youtubeReadBudget{}, nil)
	if err != nil || probe == nil || c.phase != probing {
		t.Fatalf("probe=%v phase=%v err=%v", probe, c.phase, err)
	}
	// A concurrent ordinary read discovers a newer limit while the probe runs.
	c.finish(nil, nil, &YouTubeRateLimitError{RetryAfter: time.Minute})
	if c.phase != probing {
		t.Fatal("overlapping read released the in-flight probe")
	}
	c.finish(probe, nil, nil)
	if c.phase != cooling || time.Until(c.until) <= 0 {
		t.Fatal("superseded probe cleared cooldown")
	}
}
