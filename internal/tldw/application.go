package tldw

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Config struct {
	WhisperTimeout time.Duration
}

type PromptBuilder interface {
	CreatePrompt(transcript string, metadata *VideoMetadata) (string, error)
}

// Dependencies contains the collaborators required by every Engine instance.
type Dependencies struct {
	Video   VideoAdapter
	Store   VideoStore
	AI      AIAdapter
	Prompts PromptBuilder
	Log     LogSink
}

// Engine is the application's deep module and owns workflow policy.
type Engine struct {
	video          VideoAdapter
	store          VideoStore
	ai             AIAdapter
	promptManager  PromptBuilder
	config         Config
	log            LogSink
	metadataCache  map[string]*VideoMetadata
	metadataMu     sync.RWMutex
	transcriptWork workGates
	metadataWork   workGates
}

// NewEngine initializes a fully usable application module.
func NewEngine(config Config, dependencies Dependencies) (*Engine, error) {
	if dependencies.Video == nil {
		return nil, fmt.Errorf("video adapter is required")
	}
	if dependencies.Store == nil {
		return nil, fmt.Errorf("video store is required")
	}
	if dependencies.AI == nil {
		return nil, fmt.Errorf("AI adapter is required")
	}
	if dependencies.Prompts == nil {
		return nil, fmt.Errorf("prompt builder is required")
	}
	if config.WhisperTimeout < 0 {
		return nil, fmt.Errorf("whisper timeout must not be negative")
	}
	log := dependencies.Log
	if log == nil {
		log = discardLogSink{}
	}
	app := &Engine{
		video:         dependencies.Video,
		store:         dependencies.Store,
		ai:            dependencies.AI,
		promptManager: dependencies.Prompts,
		config:        config,
		log:           log,
		metadataCache: make(map[string]*VideoMetadata),
	}
	return app, nil
}

// getCachedMetadata returns metadata from the in-memory cache if available
func (app *Engine) getCachedMetadata(id string) (*VideoMetadata, bool) {
	app.metadataMu.RLock()
	defer app.metadataMu.RUnlock()
	metadata, ok := app.metadataCache[id]
	return metadata, ok
}

// setCachedMetadata stores metadata in the in-memory cache.
func (app *Engine) setCachedMetadata(id string, metadata *VideoMetadata) {
	app.metadataMu.Lock()
	defer app.metadataMu.Unlock()
	app.metadataCache[id] = metadata
}

const negativeMetadataTTL = 15 * time.Minute

func metadataNeedsRefresh(metadata *VideoMetadata) bool {
	if metadata == nil {
		return false
	}

	if !metadata.HasCaptions && (metadata.CheckedAt.IsZero() || time.Since(metadata.CheckedAt) >= negativeMetadataTTL) {
		return true
	}
	if strings.TrimSpace(metadata.Channel) == "" {
		return true
	}
	return metadata.HasCaptions && len(metadata.CaptionLanguages) == 0
}

// MetadataFor resolves metadata for an already validated video reference.
func (app *Engine) MetadataFor(ctx context.Context, ref YouTubeRef) (*VideoMetadata, error) {
	if !validVideoRef(ref) {
		return nil, fmt.Errorf("metadata requires a valid video reference")
	}
	return app.resolveMetadata(ctx, ref)
}

// VideoTranscript holds a video's metadata and transcript
type VideoTranscript struct {
	Title       string
	Channel     string
	Duration    float64
	Description string
	Transcript  string
}

// buildPlaylistTranscript creates a structured transcript from all videos
func (app *Engine) buildPlaylistTranscript(playlistTitle string, videos []VideoTranscript) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Playlist: %s\n\n", playlistTitle)

	for i, video := range videos {
		// Format duration as minutes:seconds
		minutes := int(video.Duration / 60)
		seconds := int(video.Duration) % 60
		duration := fmt.Sprintf("%d:%02d", minutes, seconds)

		fmt.Fprintf(&sb, "Video %d of %d: %s\n", i+1, len(videos), video.Title)
		fmt.Fprintf(&sb, "Duration: %s | Channel: %s\n", duration, video.Channel)
		if video.Description != "" {
			fmt.Fprintf(&sb, "Description: %s\n", video.Description)
		}
		sb.WriteString(video.Transcript)

		// Add separator between videos (except for the last one)
		if i < len(videos)-1 {
			sb.WriteString("\n\n---\n\n")
		}
	}

	return sb.String()
}

// RefreshMetadata bypasses both metadata caches for an explicit fresh lookup.
func (app *Engine) RefreshMetadata(ctx context.Context, ref YouTubeRef) (*VideoMetadata, error) {
	if !validVideoRef(ref) {
		return nil, fmt.Errorf("metadata requires a valid video reference")
	}
	release, err := app.metadataWork.acquire(ctx, ref.ID())
	if err != nil {
		return nil, err
	}
	defer release()
	return app.fetchMetadata(ctx, ref)
}
