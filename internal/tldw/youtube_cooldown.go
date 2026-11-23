package tldw

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"
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

// YouTubeCooldown coordinates outbound reads in one video adapter. While
// healthy, reads proceed concurrently. After a 429 only one read tests recovery.
type YouTubeCooldown struct {
	mu      sync.Mutex
	until   time.Time
	backoff time.Duration
	probing bool
	changed chan struct{}
}

func (c *YouTubeCooldown) Read(ctx context.Context, fetch func() error) error {
	budget, ok := ctx.Value(youtubeBudgetKey{}).(*youtubeReadBudget)
	if !ok {
		budget = &youtubeReadBudget{waitLeft: youtubeWaitBudget}
	}
	var cause error
	for {
		probe, err := c.admit(ctx, budget, cause)
		if err != nil {
			return err
		}
		err = fetch()
		var limited *YouTubeRateLimitError
		errors.As(err, &limited)
		next := c.finish(probe, err, limited)
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

func (c *YouTubeCooldown) admit(ctx context.Context, budget *youtubeReadBudget, cause error) (time.Time, error) {
	for {
		if err := ctx.Err(); err != nil {
			return time.Time{}, err
		}
		c.mu.Lock()
		if c.until.IsZero() {
			c.mu.Unlock()
			return time.Time{}, nil
		}
		delay := time.Until(c.until)
		if delay <= 0 && !c.probing {
			c.probing = true
			probe := c.until
			c.mu.Unlock()
			return probe, nil
		}
		changed := c.changed
		c.mu.Unlock()

		allowance := budget.waitLeft
		if deadline, ok := ctx.Deadline(); ok {
			allowance = min(allowance, time.Until(deadline))
		}
		if allowance <= 0 || delay > allowance {
			return time.Time{}, &YouTubeRateLimitError{RetryAfter: max(0, delay), Cause: cause}
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
		budget.waitLeft -= time.Since(started)
	}
}

func (c *YouTubeCooldown) finish(probe time.Time, err error, limited *YouTubeRateLimitError) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if probe.IsZero() && limited == nil {
		return c.until // A read already in flight cannot clear a later 429.
	}
	if !probe.IsZero() {
		c.probing = false
	}
	switch {
	case limited != nil:
		// Overlapping failures from the same burst count as one backoff step.
		if c.backoff == 0 {
			c.backoff = 10 * time.Second
		} else if !probe.IsZero() {
			c.backoff = min(2*c.backoff, 2*time.Minute)
		}
		delay := limited.RetryAfter
		if delay <= 0 {
			delay = min(c.backoff+rand.N(c.backoff/10), 2*time.Minute)
		}
		if next := time.Now().Add(delay); next.After(c.until) {
			c.until = next
		}
	case err == nil && c.until.Equal(probe):
		c.until = time.Time{}
		c.backoff = 0
	}
	if c.changed != nil {
		close(c.changed)
	}
	c.changed = make(chan struct{})
	return c.until
}
