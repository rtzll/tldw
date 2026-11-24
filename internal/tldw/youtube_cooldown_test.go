package tldw_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestYouTubeBackoffAndRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cooldown tldw.YouTubeCooldown
		start := time.Now()
		calls := 0
		rateLimited := func(context.Context) error {
			calls++
			return &tldw.YouTubeRateLimitError{}
		}
		err := cooldown.Read(t.Context(), rateLimited)
		var limited *tldw.YouTubeRateLimitError
		if !errors.As(err, &limited) || calls != 2 || time.Since(start) < 10*time.Second || time.Since(start) > 11*time.Second {
			t.Fatalf("first call: attempts=%d elapsed=%s err=%v", calls, time.Since(start), err)
		}
		for _, want := range []time.Duration{20 * time.Second, 40 * time.Second, 80 * time.Second, 2 * time.Minute, 2 * time.Minute} {
			if limited.RetryAfter < want || limited.RetryAfter > min(want+want/10, 2*time.Minute) {
				t.Fatalf("delay = %v, want about %v", limited.RetryAfter, want)
			}
			time.Sleep(limited.RetryAfter)
			err = cooldown.Read(t.Context(), rateLimited)
			if !errors.As(err, &limited) {
				t.Fatalf("repeated limit: %v", err)
			}
		}
		time.Sleep(limited.RetryAfter)
		if err := cooldown.Read(t.Context(), func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		err = cooldown.Read(ctx, rateLimited)
		if !errors.As(err, &limited) || limited.RetryAfter < 10*time.Second || limited.RetryAfter > 11*time.Second {
			t.Fatalf("backoff after recovery: %v", err)
		}
	})
}

func startCooldown(t *testing.T, cooldown *tldw.YouTubeCooldown, delay time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := cooldown.Read(ctx, func(context.Context) error { return &tldw.YouTubeRateLimitError{RetryAfter: delay} })
	if !errors.Is(err, tldw.ErrRateLimited) {
		t.Fatalf("starting cooldown: %v", err)
	}
}

func TestYouTubeWaitBudgetIncludesWaitingBeforeFirstFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cooldown tldw.YouTubeCooldown
		startCooldown(t, &cooldown, 15*time.Second)
		start := time.Now()
		calls := 0
		err := cooldown.Read(t.Context(), func(context.Context) error {
			calls++
			return &tldw.YouTubeRateLimitError{RetryAfter: 8 * time.Second}
		})
		var limited *tldw.YouTubeRateLimitError
		if !errors.As(err, &limited) || limited.RetryAfter != 8*time.Second || calls != 1 || time.Since(start) != 15*time.Second {
			t.Fatalf("calls=%d elapsed=%s error=%v", calls, time.Since(start), err)
		}
	})
}

func TestYouTubeAllowsOneRecoveryReadAndReleasesCanceledProbe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cooldown tldw.YouTubeCooldown
		// This healthy-period request returns success late, while the provider is cooling down.
		late := make(chan struct{})
		done := make(chan error, 3)
		go func() {
			done <- cooldown.Read(t.Context(), func(context.Context) error { <-late; return nil })
		}()
		synctest.Wait()
		startCooldown(t, &cooldown, 5*time.Second)
		close(late)
		if err := <-done; err != nil {
			t.Fatal(err)
		}

		start := time.Now()
		waiter, cancelWaiter := context.WithCancel(t.Context())
		go func() {
			done <- cooldown.Read(waiter, func(context.Context) error { return errors.New("canceled waiter started fetching") })
		}()
		synctest.Wait()
		cancelWaiter()
		if err := <-done; !errors.Is(err, context.Canceled) || time.Since(start) != 0 {
			t.Fatalf("canceling during cooldown: %v", err)
		}
		probing := make(chan struct{})
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			done <- cooldown.Read(ctx, func(context.Context) error {
				close(probing)
				<-ctx.Done()
				return ctx.Err()
			})
		}()
		<-probing
		if time.Since(start) != 5*time.Second {
			t.Fatal("late success cleared the cooldown")
		}
		released := make(chan struct{})
		entered := make(chan struct{}, 2)
		for range 2 {
			go func() {
				done <- cooldown.Read(t.Context(), func(context.Context) error {
					entered <- struct{}{}
					<-released
					return nil
				})
			}()
		}
		synctest.Wait()
		if len(entered) != 0 {
			t.Fatal("another read started while a probe was in flight")
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("probe cancellation: %v", err)
		}
		synctest.Wait()
		if len(entered) != 1 {
			t.Fatalf("replacement probes = %d, want one", len(entered))
		}
		close(released)
		for range 2 {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
	})
}
