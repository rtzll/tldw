package tldw

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Config struct {
	WhisperTimeout       time.Duration
	MetadataCacheVersion int
}

type PromptBuilder interface {
	CreatePrompt(transcript string, metadata *VideoMetadata) (string, error)
}

// Engine is the application's deep module and owns workflow policy.
type Engine struct {
	video         VideoAdapter
	store         VideoStore
	ai            AIAdapter
	promptManager PromptBuilder
	config        Config
	log           LogSink
	metadataCache map[string]*VideoMetadata
	metadataMu    sync.RWMutex
}

// NewEngine initializes the application module.
func NewEngine(config Config, promptManager PromptBuilder, options ...EngineOption) *Engine {
	app := &Engine{
		promptManager: promptManager,
		config:        config,
		log:           discardLogSink{},
		metadataCache: make(map[string]*VideoMetadata),
	}
	// Apply any custom options
	for _, option := range options {
		option(app)
	}

	return app
}

// EngineOption customizes Engine creation.
type EngineOption func(*Engine)

// SetPromptManager sets a new prompt manager
func (app *Engine) SetPromptManager(pm PromptBuilder) {
	app.promptManager = pm
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

func (app *Engine) metadataRefreshReason(metadata *VideoMetadata) string {
	if metadata == nil {
		return ""
	}

	var reasons []string
	if strings.TrimSpace(metadata.Channel) == "" {
		reasons = append(reasons, "channel")
	}
	if metadata.HasCaptions && len(metadata.CaptionLanguages) == 0 {
		reasons = append(reasons, "caption languages")
	}
	if metadata.CacheVersion < app.config.MetadataCacheVersion {
		reasons = append(reasons, "metadata schema")
	}

	return strings.Join(reasons, " and ")
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
	URL         string
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
