package tldw

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/rtzll/tldw/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// YouTubeRateLimitError carries the next suggested wait and preserves the
// underlying download error for diagnostics. Zero means no delay was supplied.
type YouTubeRateLimitError struct {
	RetryAfter time.Duration
	Cause      error
}

func (e *YouTubeRateLimitError) Error() string {
	if e.RetryAfter <= 0 {
		return "YouTube is rate limiting requests. Try again shortly."
	}
	return fmt.Sprintf("YouTube is rate limiting requests. Try again in about %.0f seconds.", math.Ceil(e.RetryAfter.Seconds()))
}

func (e *YouTubeRateLimitError) Is(target error) bool { return target == ErrRateLimited }
func (e *YouTubeRateLimitError) Unwrap() error        { return e.Cause }

// A transcript's sequential metadata, refresh, and caption reads share one
// retry and one waiting allowance. Time spent doing the fetch is excluded.
type youtubeReadBudget struct {
	waitLeft time.Duration
	retried  bool
}

type youtubeBudgetKey struct{}

const youtubeWaitBudget = 20 * time.Second

func withYouTubeReadBudget(ctx context.Context) context.Context {
	return context.WithValue(ctx, youtubeBudgetKey{}, &youtubeReadBudget{waitLeft: youtubeWaitBudget})
}

type cooldownPhase uint8

const (
	healthy cooldownPhase = iota
	cooling
	probing
)

// An ordinary read has no probe. A probe may clear only the generation it tested.
type recoveryProbe struct{ generation uint64 }

// YouTubeCooldown coordinates outbound reads in one video adapter. While
// healthy, reads proceed concurrently. After a 429 only one read tests recovery.
type YouTubeCooldown struct {
	mu         sync.Mutex
	until      time.Time
	backoff    time.Duration
	phase      cooldownPhase
	generation uint64
	changed    chan struct{}
	origin     trace.SpanContext // fetch that established the active retry deadline
}

func (c *YouTubeCooldown) Read(ctx context.Context, fetch func(context.Context) error) (readErr error) {
	ctx, span := telemetry.Start(ctx, "youtube.read")
	defer func() { EndOperation(span, readErr) }()
	budget, ok := ctx.Value(youtubeBudgetKey{}).(*youtubeReadBudget)
	if !ok {
		budget = &youtubeReadBudget{waitLeft: youtubeWaitBudget}
	}
	var cause error
	for attempt := 1; ; attempt++ {
		probe, err := c.admit(ctx, budget, cause)
		if err != nil {
			return err
		}
		fetchCtx, fetchSpan := telemetry.Start(ctx, "youtube.fetch", trace.WithAttributes(attribute.Int("tldw.attempt", attempt), attribute.Bool("tldw.cooldown.probe", probe != nil)))
		err = fetch(fetchCtx)
		var limited *YouTubeRateLimitError
		errors.As(err, &limited)
		next := c.finish(probe, err, limited, fetchSpan.SpanContext())
		if limited != nil {
			fetchSpan.SetAttributes(attribute.Int64("tldw.cooldown.remaining_ms", max(0, time.Until(next).Milliseconds())))
			if limited.RetryAfter > 0 {
				fetchSpan.SetAttributes(attribute.Int64("tldw.retry_after_ms", limited.RetryAfter.Milliseconds()))
			}
		}
		EndOperation(fetchSpan, err)
		if limited == nil {
			return err
		}
		cause = limited.Cause
		if budget.retried {
			return &YouTubeRateLimitError{RetryAfter: time.Until(next), Cause: cause}
		}
		budget.retried = true
	}
}

func (c *YouTubeCooldown) admit(ctx context.Context, budget *youtubeReadBudget, cause error) (*recoveryProbe, error) {
	span := trace.SpanFromContext(ctx)
	var lastCause trace.SpanContext
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.phase == healthy {
			c.mu.Unlock()
			return nil, nil
		}
		origin := c.origin
		delay := time.Until(c.until)
		if delay <= 0 && c.phase == cooling {
			c.phase = probing
			probe := &recoveryProbe{generation: c.generation}
			c.mu.Unlock()
			linkCooldown(span, origin, lastCause)
			span.AddEvent("cooldown.probe")
			return probe, nil
		}
		changed := c.changed
		c.mu.Unlock()

		linkCooldown(span, origin, lastCause)
		lastCause = origin
		allowance := budget.waitLeft
		if deadline, ok := ctx.Deadline(); ok {
			allowance = min(allowance, time.Until(deadline))
		}
		if allowance <= 0 || delay > allowance {
			span.AddEvent("cooldown.deferred", trace.WithAttributes(attribute.Int64("tldw.cooldown.remaining_ms", max(0, delay.Milliseconds())), attribute.Int64("tldw.cooldown.budget_ms", max(0, allowance.Milliseconds()))))
			return nil, &YouTubeRateLimitError{RetryAfter: max(0, delay), Cause: cause}
		}
		wait := allowance // When a probe is running, wait for its result.
		if delay > 0 {
			wait = delay
		}
		started := time.Now()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
		case <-changed:
		case <-timer.C:
		}
		timer.Stop()
		waited := time.Since(started)
		budget.waitLeft -= waited
		span.AddEvent("cooldown.waited", trace.WithAttributes(attribute.Int64("tldw.cooldown.wait_ms", waited.Milliseconds())))
	}
}

func (c *YouTubeCooldown) finish(probe *recoveryProbe, err error, limited *YouTubeRateLimitError, origin trace.SpanContext) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if probe == nil && limited == nil {
		return c.until // A read already in flight cannot clear a later 429.
	}
	if probe != nil {
		c.phase = cooling // A failed or superseded probe releases the next probe.
	}
	switch {
	case limited != nil:
		c.generation++
		if c.phase == healthy {
			c.phase = cooling
		}
		// Overlapping failures from the same burst count as one backoff step.
		if c.backoff == 0 {
			c.backoff = 10 * time.Second
		} else if probe != nil {
			c.backoff = min(2*c.backoff, 2*time.Minute)
		}
		delay := limited.RetryAfter
		if delay <= 0 {
			delay = min(c.backoff+rand.N(c.backoff/10), 2*time.Minute)
		}
		if next := time.Now().Add(delay); next.After(c.until) {
			c.until = next
			c.origin = origin
		}
	case err == nil && probe != nil && c.generation == probe.generation:
		c.phase = healthy
		c.until = time.Time{}
		c.origin = trace.SpanContext{}
		c.backoff = 0
	}
	if c.changed != nil {
		close(c.changed)
	}
	c.changed = make(chan struct{})
	return c.until
}

// Add at most one link per observed deadline owner in this admission loop.
// Never call instrumentation while the cooldown mutex is held.
func linkCooldown(span trace.Span, origin, previous trace.SpanContext) {
	if origin.IsValid() && !origin.Equal(previous) {
		span.AddLink(trace.Link{SpanContext: origin, Attributes: []attribute.KeyValue{attribute.String("tldw.link.reason", "youtube_cooldown")}})
	}
}
