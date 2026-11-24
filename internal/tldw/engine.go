package tldw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rtzll/tldw/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TranscriptRequest describes one acquisition intent. Its zero value only uses captions.
type TranscriptRequest uint8

const (
	CaptionsOnly TranscriptRequest = iota
	TimestampedCaptions
	CaptionsThenPaid
	PaidOnly
	// TimestampedTranscript accepts any timed cache entry and rechecks negative
	// metadata on a miss. Like TimestampedCaptions it never starts paid work.
	TimestampedTranscript
)

func (r TranscriptRequest) needsTimestamps() bool {
	return r == TimestampedCaptions || r == TimestampedTranscript
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
	LoadTranscript(videoID string, source TranscriptSource) (*Transcript, error)
	SaveTranscript(transcript *Transcript) error
	LoadMetadata(videoID string) (*VideoMetadata, error)
	SaveMetadata(videoID string, metadata *VideoMetadata) error
	ListMetadata() (MetadataScan, error)
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
	transcript  TranscriptRequest
	confirmPaid func(ContentRef, *VideoMetadata) (bool, error)
}

// PlaylistTranscripts applies one acquisition intent to every item.
func PlaylistTranscripts(request TranscriptRequest) PlaylistSummaryRequest {
	return PlaylistSummaryRequest{transcript: request}
}

// PlaylistWithConsent starts each item with free captions. Approval applies
// only to the item for which the callback was invoked.
func PlaylistWithConsent(confirm func(ContentRef, *VideoMetadata) (bool, error)) PlaylistSummaryRequest {
	return PlaylistSummaryRequest{transcript: CaptionsOnly, confirmPaid: confirm}
}

type PlaylistSummaryResult struct {
	Title     string
	Markdown  string
	Processed int
	Total     int
	Declined  int
	Skipped   []string
}

// Transcript acquires a canonical transcript through one workflow shared by
// CLI, MCP, summaries, and playlists.
func (app *Engine) Transcript(ctx context.Context, ref ContentRef, request TranscriptRequest) (result *Transcript, err error) {
	ctx, span := telemetry.Start(ctx, "transcript.acquire", trace.WithAttributes(attribute.String("tldw.video.id", ref.CacheKey())))
	defer func() { EndOperation(span, err) }()
	if err := validateTranscriptRequest(request); err != nil {
		return nil, err
	}
	if !validVideoRef(ref) {
		return nil, fmt.Errorf("transcript requires a valid video reference")
	}
	key := ref.CacheKey()
	return withVideoWork(ctx, &app.transcriptWork, key, func() (*Transcript, error) {
		read := func() (*Transcript, error) { return app.loadOrAcquireTranscript(ctx, ref, request) }
		if locker, ok := app.store.(interface {
			WithTranscriptLock(context.Context, string, func() (*Transcript, error)) (*Transcript, error)
		}); ok {
			return locker.WithTranscriptLock(ctx, key, read)
		}
		return read()
	})
}

// Both locks are held before checking the cache. A previous owner may have filled it.
func (app *Engine) loadOrAcquireTranscript(ctx context.Context, ref ContentRef, request TranscriptRequest) (*Transcript, error) {
	for _, source := range request.cacheSources() {
		transcript, err := app.store.LoadTranscript(ref.CacheKey(), source)
		if errors.Is(err, ErrStoreNotFound) || errors.Is(err, ErrStoreStale) {
			continue
		}
		if err != nil {
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("tldw.cache.result", "read_error"))
			return nil, fmt.Errorf("loading cached transcript: %w", err)
		}
		if transcript != nil && transcript.Source() == source && (!request.needsTimestamps() || transcript.HasTimestamps()) {
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("tldw.cache.result", "hit"), attribute.String("tldw.transcript.source", string(source)))
			return transcript, nil
		}
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("tldw.cache.result", "miss"))
	if request == PaidOnly {
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
	metadata, err := app.resolveMetadataWithPolicy(ctx, ref, request == CaptionsThenPaid || request == TimestampedTranscript)
	if err != nil {
		return nil, fmt.Errorf("checking video metadata: %w", err)
	}
	transcript, err := readCaptions(ctx, ref, metadata, request.needsTimestamps(), app.video.FetchCaptions, app.RefreshMetadata)
	if err != nil {
		if request == CaptionsThenPaid && CanOfferPaidTranscription(err) {
			return app.transcribeVideo(ctx, ref)
		}
		return nil, err
	}

	return app.saveAcquiredTranscript(ctx, ref, transcript), nil
}

// cacheSources lists eligible origins in preference order. The empty source
// represents legacy records whose origin is unknown.
func (request TranscriptRequest) cacheSources() []TranscriptSource {
	switch request {
	case CaptionsOnly, TimestampedCaptions:
		return []TranscriptSource{TranscriptSourceCaptions}
	case PaidOnly:
		return []TranscriptSource{TranscriptSourcePaidTranscription}
	case TimestampedTranscript:
		return []TranscriptSource{TranscriptSourceCaptions, ""}
	default: // CaptionsThenPaid
		return []TranscriptSource{TranscriptSourceCaptions, TranscriptSourcePaidTranscription, ""}
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
	if err := validateTranscriptRequest(request.transcript); err != nil {
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
	missingCaptions := 0
	for i, videoRef := range playlist.Videos {
		transcript, transcriptErr := app.Transcript(ctx, videoRef, request.transcript)
		metadata, metadataErr := app.resolveMetadata(ctx, videoRef)
		if metadataErr != nil {
			metadata = &VideoMetadata{Title: fmt.Sprintf("Video %d", i+1), Channel: "Unknown", Description: "Metadata fetch failed"}
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if request.confirmPaid != nil && CanOfferPaidTranscription(transcriptErr) {
			approved, err := request.confirmPaid(videoRef, metadata)
			if err != nil {
				return result, err
			}
			if !approved {
				result.Declined++
				result.Skipped = append(result.Skipped, fmt.Sprintf("%s (%s): paid transcription declined", videoRef.ID(), metadata.Title))
				continue
			}
			transcript, transcriptErr = app.Transcript(ctx, videoRef, PaidOnly)
		}
		if transcriptErr != nil {
			if CanOfferPaidTranscription(transcriptErr) {
				missingCaptions++
			}
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s (%s): %v", videoRef.ID(), metadata.Title, transcriptErr))
			continue
		}
		plain, renderErr := transcript.Render(TranscriptRenderFormatPlain)
		if renderErr != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s (%s): %v", videoRef.ID(), metadata.Title, renderErr))
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
		result.Processed = len(videos)
	}
	if result.Processed == 0 {
		if request.transcript == CaptionsOnly && request.confirmPaid == nil && missingCaptions == result.Total {
			return result, &paidFallbackError{fmt.Errorf("%w for every video in playlist %s", ErrCaptionsUnavailable, ref.ID())}
		}
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
	return result, nil
}

func validateTranscriptRequest(request TranscriptRequest) error {
	if request > TimestampedTranscript {
		return fmt.Errorf("%w: %d", ErrInvalidTranscriptPolicy, request)
	}
	return nil
}

func (app *Engine) resolveMetadata(ctx context.Context, ref ContentRef) (*VideoMetadata, error) {
	return app.resolveMetadataWithPolicy(ctx, ref, false)
}

func (app *Engine) resolveMetadataWithPolicy(ctx context.Context, ref ContentRef, refreshNegative bool) (*VideoMetadata, error) {
	return withVideoWork(ctx, &app.metadataWork, ref.CacheKey(), func() (*VideoMetadata, error) {
		if cached, ok := app.getCachedMetadata(ref.CacheKey()); ok {
			trace.SpanFromContext(ctx).AddEvent("metadata.memory_hit")
			return app.useOrRefreshMetadata(ctx, ref, cached, refreshNegative)
		}

		if cached, err := app.store.LoadMetadata(ref.CacheKey()); err == nil {
			if cached == nil {
				return nil, fmt.Errorf("metadata store returned no record")
			}
			trace.SpanFromContext(ctx).AddEvent("metadata.disk_hit")
			resolved, refreshErr := app.useOrRefreshMetadata(ctx, ref, cached.clone(), refreshNegative)
			if refreshErr == nil {
				app.setCachedMetadata(ref.CacheKey(), resolved)
			}
			return resolved, refreshErr
		} else if !errors.Is(err, ErrStoreNotFound) && !errors.Is(err, ErrStoreStale) {
			return nil, fmt.Errorf("loading cached metadata: %w", err)
		}

		return app.fetchMetadata(ctx, ref)
	})
}

func (app *Engine) fetchMetadata(ctx context.Context, ref ContentRef) (*VideoMetadata, error) {
	trace.SpanFromContext(ctx).AddEvent("metadata.fetch")
	metadata, err := app.video.FetchMetadata(ctx, ref)
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		return nil, fmt.Errorf("video adapter returned no metadata")
	}
	metadata = metadata.clone()
	metadata.CheckedAt = time.Now()
	if err := app.store.SaveMetadata(ref.CacheKey(), metadata.clone()); err != nil {
		trace.SpanFromContext(ctx).AddEvent("metadata.cache_write_failed")
		app.log.Printf("Warning: Failed to cache metadata: %v\n", err)
	}
	app.setCachedMetadata(ref.CacheKey(), metadata)
	return metadata, nil
}

func (app *Engine) useOrRefreshMetadata(ctx context.Context, ref ContentRef, cached *VideoMetadata, refreshNegative bool) (*VideoMetadata, error) {
	if !metadataNeedsRefresh(cached, refreshNegative, time.Now()) {
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

func (app *Engine) transcribeVideo(ctx context.Context, ref ContentRef) (*Transcript, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	audioFile, err := app.video.DownloadAudio(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("downloading audio: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transcript, err := app.transcribeAudio(ctx, audioFile)
	if err != nil {
		return nil, err
	}
	return app.saveAcquiredTranscript(ctx, ref, transcript), nil
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
	return NewPaidTranscript("", text)
}

func (app *Engine) saveAcquiredTranscript(ctx context.Context, ref ContentRef, transcript *Transcript) *Transcript {
	result := *transcript
	result.VideoID = ref.CacheKey()
	_, span := telemetry.Start(ctx, "transcript.persist", trace.WithAttributes(attribute.String("tldw.transcript.source", string(result.Source()))))
	err := app.store.SaveTranscript(&result)
	EndOperation(span, err)
	if err != nil {
		trace.SpanFromContext(ctx).AddEvent("transcript.cache_write_failed")
		app.log.Printf("Warning: %v\n", err)
	}
	return &result
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
