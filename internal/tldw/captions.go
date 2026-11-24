package tldw

import (
	"context"
	"errors"
	"fmt"
	"github.com/rtzll/tldw/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"time"
)

// Only a completed caption lookup may offer paid fallback. A failed metadata
// refresh, canceled wait or rate limit never produces this outcome.
type paidFallbackError struct{ cause error }

func (e *paidFallbackError) Error() string { return e.cause.Error() }
func (e *paidFallbackError) Unwrap() error { return e.cause }

// CanOfferPaidTranscription identifies confirmed absence from a completed
// plain-text caption request. Operational failures cannot trigger a prompt.
func CanOfferPaidTranscription(err error) bool {
	var eligible *paidFallbackError
	return errors.As(err, &eligible)
}

type fetchCaptionsFunc func(context.Context, ContentRef, *VideoMetadata) (*Transcript, error)
type refreshMetadataFunc func(context.Context, ContentRef) (*VideoMetadata, error)

// readCaptions has no paid dependencies. Only the first attempt can schedule
// recovery. The final result can finish the lookup or offer plain-text fallback.
func readCaptions(ctx context.Context, ref ContentRef, metadata *VideoMetadata, timed bool, fetch fetchCaptionsFunc, refresh refreshMetadataFunc) (*Transcript, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !metadata.HasCaptions {
		err := fmt.Errorf("%w for %s", ErrCaptionsUnavailable, ref.URL())
		if metadata.Duration > 0 {
			err = fmt.Errorf("%w (duration %s)", err, time.Duration(metadata.Duration)*time.Second)
		}
		if timed {
			return nil, fmt.Errorf("%w: %w", ErrTranscriptTimestampsUnavailable, err)
		}
		return nil, &paidFallbackError{err}
	}

	attempt := func(number int, m *VideoMetadata) (*Transcript, error) {
		attemptCtx, span := telemetry.Start(ctx, "captions.acquire", trace.WithAttributes(attribute.Int("tldw.attempt", number)))
		transcript, err := fetch(attemptCtx, ref, m)
		EndOperation(span, err)
		return transcript, err
	}
	transcript, err := attempt(1, metadata)
	switch {
	case terminalTranscriptError(err):
		return nil, err
	case errors.Is(err, ErrCaptionsUnavailable):
		trace.SpanFromContext(ctx).AddEvent("captions.refresh_tracks")
		fresh, refreshErr := refresh(ctx, ref)
		if refreshErr != nil {
			return nil, fmt.Errorf("refreshing caption availability: %w", refreshErr)
		}
		if fresh.HasCaptions {
			transcript, err = attempt(2, excludingMissingTrack(fresh, err))
		} else {
			transcript, err = nil, ErrCaptionsUnavailable
		}
	case errors.Is(err, ErrDownloadFailed):
		trace.SpanFromContext(ctx).AddEvent("captions.retry_download", trace.WithAttributes(attribute.Int("tldw.retry.delay_ms", 1000)))
		if err := sleepWithContext(ctx, time.Second); err != nil {
			return nil, err
		}
		transcript, err = attempt(2, metadata)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if terminalTranscriptError(err) {
		return nil, err
	}
	if err != nil || transcript == nil {
		if err == nil {
			err = fmt.Errorf("no transcript available for %s", ref.CacheKey())
		}
		err = fmt.Errorf("fetching captions: %w", err)
		if !timed && errors.Is(err, ErrCaptionsUnavailable) {
			return nil, &paidFallbackError{err}
		}
		return nil, err
	}
	if transcript.Source() != TranscriptSourceCaptions {
		return nil, fmt.Errorf("caption adapter returned unverified transcript")
	}
	if timed && !transcript.HasTimestamps() {
		return nil, ErrTranscriptTimestampsUnavailable
	}
	return transcript, nil
}

func excludingMissingTrack(metadata *VideoMetadata, err error) *VideoMetadata {
	var missing *CaptionTrackUnavailableError
	if !errors.As(err, &missing) {
		return metadata
	}
	// The exclusion is local to this retry. Other requests can reuse the
	// original metadata, including a track that later becomes available.
	filtered := *metadata
	filtered.CaptionTracks = nil
	for _, track := range metadata.CaptionTracks {
		if track.Language != missing.Track.Language || track.Automatic != missing.Track.Automatic {
			filtered.CaptionTracks = append(filtered.CaptionTracks, track)
		}
	}
	return &filtered
}
