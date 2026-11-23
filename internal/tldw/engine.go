package tldw

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TranscriptPolicy controls whether transcript acquisition may use paid
// transcription when captions are unavailable.
type TranscriptPolicy int

const (
	TranscriptPolicyCaptionsOnly TranscriptPolicy = iota
	TranscriptPolicyCaptionsThenPaidTranscription
	TranscriptPolicyPaidTranscriptionOnly
)

// TranscriptRequest describes transcript acquisition without presentation
// concerns. Timestamp rendering is kept outside the acquisition module, but
// RequireTimestamps prevents a plain-text-only cache entry from satisfying the
// request.
type TranscriptRequest struct {
	Policy            TranscriptPolicy
	RequireTimestamps bool
}

// ErrCaptionsUnavailable is returned when captions are unavailable and the
// request does not permit paid transcription.
var ErrCaptionsUnavailable = errors.New("captions are unavailable")

// CaptionTrackUnavailableError allows a fresh lookup to exclude a missing
// exact track instead of retrying it or requesting every English variant.
type CaptionTrackUnavailableError struct {
	Track CaptionTrack
}

func (err *CaptionTrackUnavailableError) Error() string {
	return fmt.Sprintf("caption track %q is unavailable", err.Track.Language)
}

func (err *CaptionTrackUnavailableError) Unwrap() error { return ErrCaptionsUnavailable }

// ErrDownloadFailed marks a retryable failure from a video adapter.
var ErrDownloadFailed = errors.New("video download failed")

// ErrRateLimited means callers should retry later, not switch to paid transcription.
var ErrRateLimited = errors.New("video service rate limited; retry later")

// ErrInvalidTranscriptPolicy indicates a request with an unknown policy value.
var ErrInvalidTranscriptPolicy = errors.New("invalid transcript policy")

// ErrStoreNotFound indicates that a requested cache entry does not exist.
var ErrStoreNotFound = errors.New("store entry not found")

// ErrStoreStale indicates that a cache entry exists but must be refreshed.
var ErrStoreStale = errors.New("store entry is stale")

// VideoAdapter is the seam between application workflows and video access.
// Production uses yt-dlp; tests can provide a local adapter.
type VideoAdapter interface {
	FetchMetadata(ctx context.Context, ref ContentRef) (*VideoMetadata, error)
	FetchCaptions(ctx context.Context, ref ContentRef, metadata *VideoMetadata) (*Transcript, error)
	DownloadAudio(ctx context.Context, ref ContentRef) (string, error)
	FetchPlaylist(ctx context.Context, ref ContentRef) (*PlaylistInfo, error)
}

// AIAdapter is the seam for paid transcription and summary generation.
type AIAdapter interface {
	Transcribe(ctx context.Context, audioFile string) (string, error)
	Summary(ctx context.Context, prompt string) (string, error)
}

// VideoStore is the persistence seam used by application workflows.
type VideoStore interface {
	LoadTranscript(videoID string) (*Transcript, error)
	SaveTranscript(transcript *Transcript) error
	LoadMetadata(videoID string) (*VideoMetadata, error)
	SaveMetadata(videoID string, metadata *VideoMetadata) error
	ListMetadata() ([]StoredVideoMetadata, error)
}

// LogSink receives diagnostic events without coupling workflows to a terminal.
type LogSink interface {
	Printf(format string, args ...any)
}

type discardLogSink struct{}

func (discardLogSink) Printf(string, ...any) {}

// Summary is transport-neutral output. Terminal rendering belongs to the CLI
// adapter and structured serialization belongs to MCP.
type Summary struct {
	Markdown string
}

type PlaylistSummaryRequest struct {
	Transcript               TranscriptRequest
	ConfirmPaidTranscription func(ref ContentRef, metadata *VideoMetadata) bool
}

type PlaylistSummaryResult struct {
	Title     string
	Markdown  string
	Processed int
	Total     int
	Skipped   []string
}

// Transcript acquires a canonical transcript through one workflow shared by
// CLI, MCP, summaries, and playlists.
func (app *Engine) Transcript(ctx context.Context, ref ContentRef, request TranscriptRequest) (*Transcript, error) {
	if err := validateTranscriptRequest(request); err != nil {
		return nil, err
	}
	if !validVideoRef(ref) {
		return nil, fmt.Errorf("transcript requires a valid video reference")
	}
	// Recheck the cache after acquiring the video gate; another request may have filled it.
	release, err := app.transcriptWork.acquire(ctx, ref.CacheKey())
	if err != nil {
		return nil, err
	}
	defer release()
	// Stores that share a filesystem may coordinate with other processes.
	if locker, ok := app.store.(interface {
		LockTranscript(context.Context, string) (func() error, error)
	}); ok {
		unlock, err := locker.LockTranscript(ctx, ref.CacheKey())
		if err != nil {
			return nil, fmt.Errorf("locking transcript: %w", err)
		}
		defer func() {
			if err := unlock(); err != nil {
				app.log.Printf("Warning: unlocking transcript: %v\n", err)
			}
		}()
	}
	if transcript, err := app.store.LoadTranscript(ref.CacheKey()); err == nil {
		if cachedTranscriptAllowed(transcript, request) {
			return transcript, nil
		}
	} else if !errors.Is(err, ErrStoreNotFound) && !errors.Is(err, ErrStoreStale) {
		return nil, fmt.Errorf("loading cached transcript: %w", err)
	}
	if request.Policy == TranscriptPolicyPaidTranscriptionOnly {
		// An X post URL alone cannot establish whether it contains one video.
		if ref.IsX() {
			if _, err := app.resolveMetadata(ctx, ref); err != nil {
				return nil, fmt.Errorf("checking X post: %w", err)
			}
		}
		return app.transcribeVideo(ctx, ref)
	}

	if !ref.IsX() {
		ctx = withYouTubeReadBudget(ctx)
	}
	metadata, err := app.resolveMetadataWithPolicy(ctx, ref, request.Policy == TranscriptPolicyCaptionsThenPaidTranscription)
	if err != nil {
		return nil, fmt.Errorf("checking video metadata: %w", err)
	}
	if !metadata.HasCaptions {
		if request.Policy == TranscriptPolicyCaptionsOnly || request.RequireTimestamps {
			err := fmt.Errorf("%w for %s", ErrCaptionsUnavailable, ref.URL())
			if metadata.Duration > 0 {
				err = fmt.Errorf("%w (duration %s)", err, time.Duration(metadata.Duration)*time.Second)
			}
			if request.RequireTimestamps {
				return nil, fmt.Errorf("%w: %w", ErrTranscriptTimestampsUnavailable, err)
			}
			return nil, err
		}
		return app.transcribeVideo(ctx, ref)
	}

	transcript, err := app.video.FetchCaptions(ctx, ref, metadata)
	if terminalTranscriptError(err) {
		return nil, err
	}
	if errors.Is(err, ErrCaptionsUnavailable) {
		var missing *CaptionTrackUnavailableError
		errors.As(err, &missing)
		fresh, refreshErr := app.RefreshMetadata(ctx, ref)
		if refreshErr != nil {
			return nil, fmt.Errorf("refreshing caption availability: %w", refreshErr)
		}
		// Do not mutate metadata shared with concurrent readers or cache a
		// temporary exclusion of a track that may return on a later request.
		retryMetadata := *fresh
		if missing != nil {
			retryMetadata.CaptionTracks = nil
			for _, track := range fresh.CaptionTracks {
				if track.Language != missing.Track.Language || track.Automatic != missing.Track.Automatic {
					retryMetadata.CaptionTracks = append(retryMetadata.CaptionTracks, track)
				}
			}
		}
		if fresh.HasCaptions {
			transcript, err = app.video.FetchCaptions(ctx, ref, &retryMetadata)
		} else {
			transcript, err = nil, ErrCaptionsUnavailable
		}
	} else if errors.Is(err, ErrDownloadFailed) {
		if waitErr := sleepWithContext(ctx, time.Second); waitErr != nil {
			return nil, waitErr
		}
		transcript, err = app.video.FetchCaptions(ctx, ref, metadata)
	}
	if terminalTranscriptError(err) {
		return nil, err
	}
	// On X, a failed caption download does not establish that captions are absent.
	allowFallback := !ref.IsX() || errors.Is(err, ErrCaptionsUnavailable)
	if (err != nil || transcript == nil) && allowFallback && request.Policy == TranscriptPolicyCaptionsThenPaidTranscription && !request.RequireTimestamps {
		return app.transcribeVideo(ctx, ref)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching captions: %w", err)
	}
	if transcript == nil {
		return nil, fmt.Errorf("no transcript available for %s", ref.CacheKey())
	}
	if request.RequireTimestamps && !transcript.HasTimestamps() {
		return nil, ErrTranscriptTimestampsUnavailable
	}

	transcript.VideoID = ref.CacheKey()
	if err := app.persistTranscript(transcript); err != nil {
		app.log.Printf("Warning: %v\n", err)
	}
	return transcript, nil
}

func cachedTranscriptAllowed(transcript *Transcript, request TranscriptRequest) bool {
	if transcript == nil || (request.RequireTimestamps && !transcript.HasTimestamps()) {
		return false
	}
	switch request.Policy {
	case TranscriptPolicyCaptionsOnly:
		return transcript.Source == TranscriptSourceCaptions
	case TranscriptPolicyPaidTranscriptionOnly:
		return transcript.Source == TranscriptSourcePaidTranscription
	default:
		return true
	}
}

// SummarizeVideo acquires a transcript and returns raw Markdown without
// transport-specific rendering or output.
func (app *Engine) SummarizeVideo(ctx context.Context, ref ContentRef, request TranscriptRequest) (Summary, error) {
	transcript, err := app.Transcript(ctx, ref, request)
	if err != nil {
		return Summary{}, err
	}
	plain, err := transcript.Render(TranscriptRenderFormatPlain)
	if err != nil {
		return Summary{}, err
	}
	metadata, err := app.resolveMetadata(ctx, ref)
	if err != nil {
		app.log.Printf("Failed to extract video metadata: %v\n", err)
		metadata = nil
	}
	prompt, err := app.promptManager.CreatePrompt(plain, metadata)
	if err != nil {
		return Summary{}, fmt.Errorf("creating prompt: %w", err)
	}
	markdown, err := app.ai.Summary(ctx, prompt)
	if err != nil {
		return Summary{}, fmt.Errorf("generating summary: %w", err)
	}
	return Summary{Markdown: markdown}, nil
}

// CreatePlaylistSummary owns playlist traversal and transcript acquisition but
// returns transport-neutral output. A caller may provide a consent callback;
// the application module never prompts or prints directly.
func (app *Engine) CreatePlaylistSummary(ctx context.Context, ref ContentRef, request PlaylistSummaryRequest) (PlaylistSummaryResult, error) {
	if err := validateTranscriptRequest(request.Transcript); err != nil {
		return PlaylistSummaryResult{}, err
	}
	if !ref.IsPlaylist() {
		return PlaylistSummaryResult{}, fmt.Errorf("playlist summary requires a playlist reference")
	}
	playlist, err := app.video.FetchPlaylist(ctx, ref)
	if err != nil {
		return PlaylistSummaryResult{}, fmt.Errorf("extracting playlist videos: %w", err)
	}
	if playlist == nil {
		return PlaylistSummaryResult{}, fmt.Errorf("extracting playlist videos: adapter returned no playlist")
	}
	if len(playlist.Videos) == 0 {
		return PlaylistSummaryResult{}, fmt.Errorf("no videos found in playlist")
	}

	result := PlaylistSummaryResult{Title: playlist.Title, Total: len(playlist.Videos)}
	var videos []VideoTranscript
	for i, videoRef := range playlist.Videos {
		transcript, transcriptErr := app.Transcript(ctx, videoRef, request.Transcript)
		metadata, metadataErr := app.resolveMetadata(ctx, videoRef)
		if metadataErr != nil {
			metadata = &VideoMetadata{Title: fmt.Sprintf("Video %d", i+1), Channel: "Unknown", Description: "Metadata fetch failed"}
		}
		if errors.Is(transcriptErr, ErrCaptionsUnavailable) && request.ConfirmPaidTranscription != nil && request.ConfirmPaidTranscription(videoRef, metadata) {
			transcript, transcriptErr = app.Transcript(ctx, videoRef, TranscriptRequest{Policy: TranscriptPolicyPaidTranscriptionOnly})
		}
		if transcriptErr != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("Video %d: %s (transcript error)", i+1, metadata.Title))
			continue
		}
		plain, renderErr := transcript.Render(TranscriptRenderFormatPlain)
		if renderErr != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("Video %d: %s (transcript render error)", i+1, metadata.Title))
			continue
		}
		description := metadata.Description
		if len(description) > 150 {
			description = description[:147] + "..."
		}
		videos = append(videos, VideoTranscript{
			Title:       metadata.Title,
			Channel:     metadata.Channel,
			Duration:    metadata.Duration,
			Description: description,
			Transcript:  plain,
		})
	}
	if len(videos) == 0 {
		return result, fmt.Errorf("no video transcripts could be obtained")
	}

	combined := app.buildPlaylistTranscript(playlist.Title, videos)
	prompt, err := app.promptManager.CreatePrompt(combined, nil)
	if err != nil {
		return result, fmt.Errorf("creating prompt: %w", err)
	}
	result.Markdown, err = app.ai.Summary(ctx, prompt)
	if err != nil {
		return result, fmt.Errorf("generating playlist summary: %w", err)
	}
	result.Processed = len(videos)
	return result, nil
}

func validateTranscriptRequest(request TranscriptRequest) error {
	switch request.Policy {
	case TranscriptPolicyCaptionsOnly, TranscriptPolicyCaptionsThenPaidTranscription, TranscriptPolicyPaidTranscriptionOnly:
	default:
		return fmt.Errorf("%w: %d", ErrInvalidTranscriptPolicy, request.Policy)
	}
	if request.RequireTimestamps && request.Policy == TranscriptPolicyPaidTranscriptionOnly {
		return ErrTranscriptTimestampsUnavailable
	}
	return nil
}

func (app *Engine) resolveMetadata(ctx context.Context, ref ContentRef) (*VideoMetadata, error) {
	return app.resolveMetadataWithPolicy(ctx, ref, false)
}

func (app *Engine) resolveMetadataWithPolicy(ctx context.Context, ref ContentRef, refreshNegative bool) (*VideoMetadata, error) {
	release, err := app.metadataWork.acquire(ctx, ref.CacheKey())
	if err != nil {
		return nil, err
	}
	defer release()
	if cached, ok := app.getCachedMetadata(ref.CacheKey()); ok {
		return app.useOrRefreshMetadata(ctx, ref, cached, refreshNegative)
	}

	if cached, err := app.store.LoadMetadata(ref.CacheKey()); err == nil {
		resolved, refreshErr := app.useOrRefreshMetadata(ctx, ref, cached, refreshNegative)
		if refreshErr == nil {
			app.setCachedMetadata(ref.CacheKey(), resolved)
		}
		return resolved, refreshErr
	} else if !errors.Is(err, ErrStoreNotFound) && !errors.Is(err, ErrStoreStale) {
		return nil, fmt.Errorf("loading cached metadata: %w", err)
	}

	return app.fetchMetadata(ctx, ref)
}

func (app *Engine) fetchMetadata(ctx context.Context, ref ContentRef) (*VideoMetadata, error) {
	metadata, err := app.video.FetchMetadata(ctx, ref)
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		return nil, fmt.Errorf("video adapter returned no metadata")
	}
	app.cacheMetadata(ref.CacheKey(), metadata)
	return metadata, nil
}

func (app *Engine) useOrRefreshMetadata(ctx context.Context, ref ContentRef, cached *VideoMetadata, refreshNegative bool) (*VideoMetadata, error) {
	if !metadataNeedsRefresh(cached) && (!refreshNegative || cached.HasCaptions) {
		return cached, nil
	}
	refreshed, err := app.fetchMetadata(ctx, ref)
	if err != nil {
		// A failed lookup must not turn stale absence into a decision to charge for transcription.
		if !cached.HasCaptions || terminalTranscriptError(err) {
			return nil, err
		}
		app.log.Printf("Warning: metadata refresh failed: %v\n", err)
		return cached, nil
	}
	return refreshed, nil
}

func (app *Engine) cacheMetadata(videoID string, metadata *VideoMetadata) {
	metadata.CheckedAt = time.Now()
	if err := app.store.SaveMetadata(videoID, metadata); err != nil {
		app.log.Printf("Warning: Failed to cache metadata: %v\n", err)
	}
	app.setCachedMetadata(videoID, metadata)
}

func (app *Engine) transcribeVideo(ctx context.Context, ref ContentRef) (*Transcript, error) {
	audioFile, err := app.video.DownloadAudio(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("downloading audio: %w", err)
	}
	transcript, err := app.transcribeAudio(ctx, audioFile)
	if err != nil {
		return nil, err
	}
	transcript.VideoID = ref.CacheKey()
	if err := app.persistTranscript(transcript); err != nil {
		app.log.Printf("Warning: %v\n", err)
	}
	return transcript, nil
}

func (app *Engine) transcribeAudio(ctx context.Context, audioFile string) (*Transcript, error) {
	if app.config.TranscriptionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, app.config.TranscriptionTimeout)
		defer cancel()
	}
	text, err := app.ai.Transcribe(ctx, audioFile)
	if err != nil {
		return nil, err
	}
	return &Transcript{Source: TranscriptSourcePaidTranscription, Text: strings.TrimSpace(text)}, nil
}

func (app *Engine) persistTranscript(transcript *Transcript) error {
	return app.store.SaveTranscript(transcript)
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func terminalTranscriptError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrRateLimited)
}
