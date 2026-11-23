package tldw

import (
	"context"
	"errors"
	"fmt"
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
	return errors.As(err, &eligible) && errors.Is(eligible.cause, ErrCaptionsUnavailable)
}

type fetchCaptionsFunc func(context.Context, ContentRef, *VideoMetadata) (*Transcript, error)
type refreshMetadataFunc func(context.Context, ContentRef) (*VideoMetadata, error)

// readCaptions has no paid dependencies. Only the first attempt can schedule
// recovery. The final result can finish the lookup or offer plain-text fallback.
func readCaptions(ctx context.Context, ref ContentRef, metadata *VideoMetadata, timed bool, fetch fetchCaptionsFunc, refresh refreshMetadataFunc) (*Transcript, error) {
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

	transcript, err := fetch(ctx, ref, metadata)
	switch {
	case terminalTranscriptError(err):
		return nil, err
	case errors.Is(err, ErrCaptionsUnavailable):
		fresh, refreshErr := refresh(ctx, ref)
		if refreshErr != nil {
			return nil, fmt.Errorf("refreshing caption availability: %w", refreshErr)
		}
		if fresh.HasCaptions {
			transcript, err = fetch(ctx, ref, excludingMissingTrack(fresh, err))
		} else {
			transcript, err = nil, ErrCaptionsUnavailable
		}
	case errors.Is(err, ErrDownloadFailed):
		if err := sleepWithContext(ctx, time.Second); err != nil {
			return nil, err
		}
		transcript, err = fetch(ctx, ref, metadata)
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
		if !timed && (!ref.IsX() || errors.Is(err, ErrCaptionsUnavailable)) {
			return nil, &paidFallbackError{err}
		}
		return nil, err
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
