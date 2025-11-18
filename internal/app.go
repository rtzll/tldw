package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// App holds the application state and dependencies
type App struct {
	youtube       *YouTube
	audio         *Audio
	ai            *AI
	promptManager *PromptManager
	config        *Config
	ui            UIManager
	metadataCache map[string]*VideoMetadata
	metadataMu    sync.RWMutex
}

// NewApp initializes the application
func NewApp(config *Config, options ...AppOption) *App {
	cmdRunner := &DefaultCommandRunner{}

	promptManager := NewPromptManager(config.ConfigDir, config.Prompt)
	audio := NewAudio(cmdRunner, config.TempDir, config.Verbose)

	ui := NewUIManager(config.Verbose, config.Quiet)

	app := &App{
		youtube:       NewYouTube(os.DirFS("."), config.TranscriptsDir, config.Verbose, config.Quiet),
		audio:         audio,
		ai:            NewAIWithKey(config.OpenAIAPIKey, audio, config.TLDRModel, WhisperLimit, config.SummaryTimeout, config.Verbose, config.Quiet),
		promptManager: promptManager,
		config:        config,
		ui:            ui,
		metadataCache: make(map[string]*VideoMetadata),
	}

	// Apply any custom options
	for _, option := range options {
		option(app)
	}

	return app
}

// AppOption customizes App creation
type AppOption func(*App)

// WithYouTube sets a custom YouTube downloader
func WithYouTube(youtube *YouTube) AppOption {
	return func(a *App) {
		a.youtube = youtube
	}
}

// WithAudio sets a custom audio processor
func WithAudio(audio *Audio) AppOption {
	return func(a *App) {
		a.audio = audio
	}
}

// WithAI sets a custom AI processor
func WithAI(ai *AI) AppOption {
	return func(a *App) {
		a.ai = ai
	}
}

// SetPromptManager sets a new prompt manager
func (app *App) SetPromptManager(pm *PromptManager) {
	app.promptManager = pm
}

// shouldShowStatus returns true if status indicators should be shown
func (app *App) shouldShowStatus() bool {
	return !app.config.Quiet && !app.config.Verbose
}

// Printf outputs formatted text only if not in quiet mode
func (app *App) Printf(format string, args ...interface{}) {
	if !app.config.Quiet {
		fmt.Printf(format, args...)
	}
}

// Println outputs text only if not in quiet mode
func (app *App) Println(args ...interface{}) {
	if !app.config.Quiet {
		fmt.Println(args...)
	}
}

// PrintResult outputs the final result - always shows
func (app *App) PrintResult(args ...interface{}) {
	fmt.Println(args...)
}

// VerbosePrintf outputs formatted text only if verbose mode is enabled and not quiet
func (app *App) VerbosePrintf(format string, args ...interface{}) {
	if app.config.Verbose && !app.config.Quiet {
		fmt.Printf(format, args...)
	}
}

// newSpinner returns a spinner if status should be shown, otherwise a no-op progress bar
func (app *App) newSpinner(description string) ProgressBar {
	if app.shouldShowStatus() {
		return app.ui.NewSpinner(description)
	}
	return &NoOpProgressBar{}
}

// WorkflowProgress manages all console output for a single workflow
type WorkflowProgress struct {
	spinner ProgressBar
	verbose bool
	quiet   bool
}

// newWorkflowProgress creates a workflow progress manager - SINGLE point of console control
func (app *App) newWorkflowProgress(initialDescription string) *WorkflowProgress {
	var spinner ProgressBar
	if app.shouldShowStatus() {
		spinner = app.ui.NewSpinner(initialDescription)
	} else {
		spinner = &NoOpProgressBar{}
	}

	return &WorkflowProgress{
		spinner: spinner,
		verbose: app.config.Verbose,
		quiet:   app.config.Quiet,
	}
}

// getCachedMetadata returns metadata from the in-memory cache if available
func (app *App) getCachedMetadata(id string) (*VideoMetadata, bool) {
	app.metadataMu.RLock()
	defer app.metadataMu.RUnlock()
	metadata, ok := app.metadataCache[id]
	return metadata, ok
}

// setCachedMetadata stores metadata in the in-memory cache
func (app *App) setCachedMetadata(id string, metadata *VideoMetadata) {
	app.metadataMu.Lock()
	defer app.metadataMu.Unlock()
	app.metadataCache[id] = metadata
}

// UpdateStatus updates the workflow status (replaces all spinner.Describe calls)
func (wp *WorkflowProgress) UpdateStatus(description string) {
	wp.spinner.Describe(description)
	if wp.verbose {
		// In verbose mode, also print to stdout for logging
		fmt.Printf("[Status] %s\n", description)
	}
}

// Log outputs verbose information (replaces all fmt.Printf calls)
func (wp *WorkflowProgress) Log(format string, args ...interface{}) {
	if wp.verbose {
		fmt.Printf(format, args...)
	}
}

// Finish completes the workflow
func (wp *WorkflowProgress) Finish() {
	wp.spinner.Finish()
}

// PauseForUserInput temporarily clears the spinner (useful before user prompts)
func (wp *WorkflowProgress) PauseForUserInput() {
	// Clear the spinner display without finishing it
	if visibleSpinner, ok := wp.spinner.(*VisibleProgressBar); ok {
		visibleSpinner.Clear()
	}
}

// DownloadAudio downloads audio from a YouTube URL and returns the file path
func (app *App) DownloadAudio(ctx context.Context, youtubeURL string) (string, error) {
	return app.DownloadAudioWithProgress(ctx, youtubeURL, false)
}

// DownloadAudioWithProgress downloads audio with optional progress tracking
func (app *App) DownloadAudioWithProgress(ctx context.Context, youtubeURL string, showProgress bool) (string, error) {
	if err := EnsureDirs(app.config.CacheDir); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	var progressBar ProgressBar
	if showProgress {
		progressBar = app.ui.NewProgressBar(100, "Downloading audio")
	}

	audioFile, err := app.youtube.AudioWithProgress(ctx, youtubeURL, progressBar)
	if err != nil {
		return "", fmt.Errorf("downloading audio: %w", err)
	}

	return audioFile, nil
}

// TranscribeAudio transcribes an audio file and returns the transcript
func (app *App) TranscribeAudio(ctx context.Context, audioFile string) (string, error) {
	return app.TranscribeAudioWithProgress(ctx, audioFile, false)
}

// TranscribeAudioWithProgress transcribes an audio file with optional progress bar
func (app *App) TranscribeAudioWithProgress(ctx context.Context, audioFile string, showProgress bool) (string, error) {
	var progressBar ProgressBar
	if showProgress {
		// Create progress bar through UIManager
		progressBar = app.ui.NewProgressBar(100, "Transcribing audio") // Will adjust total based on chunks
	}

	transcript, err := app.ai.TranscribeWithProgress(ctx, audioFile, progressBar)
	if err != nil {
		return "", err
	}

	return transcript, nil
}

// GetTranscript gets transcript from YouTube (cached or downloaded)
func (app *App) GetTranscript(ctx context.Context, youtubeURL string) (string, error) {
	return app.GetTranscriptWithStatus(ctx, youtubeURL, app.shouldShowStatus())
}

// GetTranscriptWithStatus gets transcript with optional status spinner
func (app *App) GetTranscriptWithStatus(ctx context.Context, youtubeURL string, showStatus bool) (string, error) {
	var spinner ProgressBar
	if showStatus {
		spinner = app.newSpinner("Checking for existing captions...")
	} else {
		spinner = &NoOpProgressBar{}
	}
	defer spinner.Finish()

	if err := EnsureDirs(app.config.TranscriptsDir); err != nil {
		return "", fmt.Errorf("creating transcripts directory: %w", err)
	}

	_, youtubeID := ParseArg(youtubeURL)
	existingTranscriptPath := filepath.Join(app.config.TranscriptsDir, youtubeID+".txt")

	// Check for cached transcript
	if FileExists(existingTranscriptPath) {
		spinner.Describe("Found cached transcript")
		app.VerbosePrintf("Found existing transcript for %s\n", youtubeID)
		text, err := os.ReadFile(existingTranscriptPath)
		if err != nil {
			return "", fmt.Errorf("reading existing transcript: %w", err)
		}
		return string(text), nil
	}

	spinner.Describe("Checking caption availability...")
	spinner.Advance()

	// Check metadata first to see if captions are available (faster than attempting download)
	metadata, err := app.MetadataWithStatus(ctx, youtubeURL, false) // Don't show nested spinner
	if err != nil {
		return "", fmt.Errorf("checking video metadata: %w", err)
	}

	// If no captions available, skip the expensive download attempt
	if !metadata.HasCaptions {
		return "", fmt.Errorf("no captions available for %s", youtubeID)
	}

	spinner.Describe("Fetching YouTube captions...")
	spinner.Advance()
	app.VerbosePrintf("Fetching transcript for %s\n", youtubeID)

	// Try to get transcript from YouTube (we know captions exist)
	transcript, err := app.youtube.FetchTranscript(ctx, youtubeURL)
	if err != nil || transcript == "" {
		// Only retry if it's a download failure (not other errors like invalid ID)
		if errors.Is(err, ErrDownloadFailed) {
			spinner.Describe("Download failed, retrying...")
			app.VerbosePrintf("Download failed, retrying in 1 second...\n")
			time.Sleep(1 * time.Second)

			transcript, err = app.youtube.FetchTranscript(ctx, youtubeURL)
		}

		if err != nil || transcript == "" {
			return "", fmt.Errorf("no transcript available for %s", youtubeID)
		}
	}

	return transcript, nil
}

// Metadata gets metadata from YouTube (cached or fresh)
func (app *App) Metadata(ctx context.Context, youtubeURL string) (*VideoMetadata, error) {
	return app.MetadataWithStatus(ctx, youtubeURL, app.shouldShowStatus())
}

// MetadataWithStatus gets metadata with optional status spinner
func (app *App) MetadataWithStatus(ctx context.Context, youtubeURL string, showStatus bool) (*VideoMetadata, error) {
	var spinner ProgressBar
	if showStatus {
		spinner = app.newSpinner("Fetching video metadata...")
	} else {
		spinner = &NoOpProgressBar{}
	}
	defer spinner.Finish()

	_, youtubeID := ParseArg(youtubeURL)

	// Check in-memory cache first
	if cachedMetadata, ok := app.getCachedMetadata(youtubeID); ok {
		if showStatus {
			spinner.Describe("Using in-memory metadata")
		}
		app.VerbosePrintf("Using in-memory metadata for %s\n", youtubeID)
		return cachedMetadata, nil
	}

	// Try to load cached metadata first
	if cachedMetadata, err := LoadCachedMetadata(youtubeID, app.config.TranscriptsDir); err == nil {
		spinner.Describe("Found cached metadata")
		app.VerbosePrintf("Using cached metadata for %s\n", youtubeID)
		app.setCachedMetadata(youtubeID, cachedMetadata)
		return cachedMetadata, nil
	}

	// No cache found, fetch from YouTube
	spinner.Describe("Fetching video metadata from YouTube...")
	spinner.Advance()
	app.VerbosePrintf("Fetching fresh metadata for %s\n", youtubeID)

	metadata, err := app.youtube.Metadata(ctx, youtubeURL)
	if err != nil {
		return nil, err
	}

	// Cache the metadata for future use
	spinner.Describe("Caching metadata...")
	spinner.Advance()
	if err := SaveMetadata(youtubeID, metadata, app.config.TranscriptsDir); err != nil {
		app.VerbosePrintf("Warning: Failed to cache metadata: %v\n", err)
	}
	app.setCachedMetadata(youtubeID, metadata)

	return metadata, nil
}

// GenerateSummary creates a summary from transcript and returns it
func (app *App) GenerateSummary(ctx context.Context, youtubeURL, transcript string) (string, error) {
	return app.GenerateSummaryWithStatus(ctx, youtubeURL, transcript, false)
}

// GenerateSummaryWithStatus creates a summary with optional status display
func (app *App) GenerateSummaryWithStatus(ctx context.Context, youtubeURL, transcript string, showStatus bool) (string, error) {
	if transcript == "" {
		return "", fmt.Errorf("transcript is empty")
	}

	var spinner ProgressBar
	if showStatus {
		spinner = app.newSpinner("Generating summary with OpenAI...")
	} else {
		spinner = &NoOpProgressBar{}
	}
	defer spinner.Finish()

	var metadata *VideoMetadata
	metadata, err := app.Metadata(ctx, youtubeURL)
	if err != nil {
		app.VerbosePrintf("Failed to extract video metadata: %v\n", err)
	}

	spinner.Describe("Creating prompt...")
	spinner.Advance()

	// Create the prompt using the PromptManager
	prompt, err := app.promptManager.CreatePrompt(transcript, metadata)
	if err != nil {
		return "", fmt.Errorf("creating prompt: %w", err)
	}

	spinner.Describe("Generating summary with OpenAI...")
	spinner.Advance()

	// Get raw summary content from AI
	summaryContent, err := app.ai.Summary(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("generating summary: %w", err)
	}

	spinner.Describe("Rendering summary...")
	spinner.Advance()

	// Render the summary content with glamour
	renderedSummary, err := RenderMarkdown(summaryContent)
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}

	return renderedSummary, nil
}

// SummarizeYouTube performs the complete workflow: get transcript -> summarize
func (app *App) SummarizeYouTube(ctx context.Context, youtubeURL string, fallbackWhisper bool) error {
	// Check if this is a playlist
	_, id := ParseArg(youtubeURL)
	if IsValidPlaylistID(id) {
		return app.SummarizePlaylist(ctx, youtubeURL, fallbackWhisper)
	}

	// Create SINGLE workflow progress manager - consolidates ALL console output
	progress := app.newWorkflowProgress("Processing video...")
	defer progress.Finish()

	// Get transcript with consolidated progress
	progress.UpdateStatus("Getting transcript...")
	transcript, err := app.getTranscriptWithProgressManager(ctx, youtubeURL, fallbackWhisper, progress)
	if err != nil {
		return err
	}

	// Generate summary with consolidated progress
	progress.UpdateStatus("Generating summary with OpenAI...")
	summary, err := app.generateSummaryWithProgressManager(ctx, youtubeURL, transcript, progress)
	if err != nil {
		return err
	}

	progress.Finish() // Finish spinner before printing result
	app.PrintResult(summary)
	return nil
}

// getTranscriptWithProgressManager gets transcript using consolidated progress manager
func (app *App) getTranscriptWithProgressManager(ctx context.Context, youtubeURL string, fallbackWhisper bool, progress *WorkflowProgress) (string, error) {
	_, youtubeID := ParseArg(youtubeURL)

	// Check for existing transcript
	if err := EnsureDirs(app.config.TranscriptsDir); err != nil {
		return "", fmt.Errorf("creating transcripts directory: %w", err)
	}

	existingTranscriptPath := filepath.Join(app.config.TranscriptsDir, youtubeID+".txt")
	if FileExists(existingTranscriptPath) {
		progress.Log("Found existing transcript for %s\n", youtubeID)
		text, err := os.ReadFile(existingTranscriptPath)
		if err != nil {
			return "", fmt.Errorf("reading existing transcript: %w", err)
		}
		return string(text), nil
	}

	// Check if captions are available
	progress.UpdateStatus("Checking caption availability...")
	metadata, err := app.metadataWithProgressManager(ctx, youtubeURL, progress)
	if err != nil {
		return "", fmt.Errorf("checking video metadata: %w", err)
	}

	if !metadata.HasCaptions {
		// No captions, fall back to Whisper
		return app.handleWhisperFallbackWithProgressManager(ctx, youtubeURL, fallbackWhisper, progress)
	}

	// Try to get transcript from YouTube
	progress.UpdateStatus("Fetching YouTube captions...")
	progress.Log("Fetching transcript for %s\n", youtubeID)

	transcript, err := app.youtube.FetchTranscript(ctx, youtubeURL)
	if err != nil || transcript == "" {
		// Retry once if download failed
		if errors.Is(err, ErrDownloadFailed) {
			progress.UpdateStatus("Download failed, retrying...")
			progress.Log("Download failed, retrying in 1 second...\n")
			time.Sleep(1 * time.Second)
			transcript, err = app.youtube.FetchTranscript(ctx, youtubeURL)
		}

		if err != nil || transcript == "" {
			return "", fmt.Errorf("no transcript available for %s", youtubeID)
		}
	}

	return transcript, nil
}

// generateSummaryWithProgressManager generates summary using consolidated progress manager
func (app *App) generateSummaryWithProgressManager(ctx context.Context, youtubeURL, transcript string, progress *WorkflowProgress) (string, error) {
	if transcript == "" {
		return "", fmt.Errorf("transcript is empty")
	}

	// Get metadata
	metadata, err := app.metadataWithProgressManager(ctx, youtubeURL, progress)
	if err != nil {
		progress.Log("Failed to extract video metadata: %v\n", err)
	}

	// Create prompt
	progress.UpdateStatus("Creating prompt...")
	prompt, err := app.promptManager.CreatePrompt(transcript, metadata)
	if err != nil {
		return "", fmt.Errorf("creating prompt: %w", err)
	}

	// Generate summary
	progress.UpdateStatus("Generating summary with OpenAI...")
	summaryContent, err := app.ai.Summary(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("generating summary: %w", err)
	}

	// Render markdown
	progress.UpdateStatus("Rendering summary...")
	renderedSummary, err := RenderMarkdown(summaryContent)
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}

	return renderedSummary, nil
}

// metadataWithProgressManager gets metadata using consolidated progress manager
func (app *App) metadataWithProgressManager(ctx context.Context, youtubeURL string, progress *WorkflowProgress) (*VideoMetadata, error) {
	_, youtubeID := ParseArg(youtubeURL)

	// In-memory cache first
	if cachedMetadata, ok := app.getCachedMetadata(youtubeID); ok {
		progress.Log("Using in-memory metadata for %s\n", youtubeID)
		return cachedMetadata, nil
	}

	// Try cached metadata first
	if cachedMetadata, err := LoadCachedMetadata(youtubeID, app.config.TranscriptsDir); err == nil {
		progress.Log("Using cached metadata for %s\n", youtubeID)
		app.setCachedMetadata(youtubeID, cachedMetadata)
		return cachedMetadata, nil
	}

	// Fetch from YouTube
	progress.Log("Fetching fresh metadata for %s\n", youtubeID)
	metadata, err := app.youtube.Metadata(ctx, youtubeURL)
	if err != nil {
		return nil, err
	}

	// Cache metadata
	if err := SaveMetadata(youtubeID, metadata, app.config.TranscriptsDir); err != nil {
		progress.Log("Warning: Failed to cache metadata: %v\n", err)
	}
	app.setCachedMetadata(youtubeID, metadata)

	return metadata, nil
}

// handleWhisperFallbackWithProgressManager handles Whisper fallback with progress manager
func (app *App) handleWhisperFallbackWithProgressManager(ctx context.Context, youtubeURL string, fallbackWhisper bool, progress *WorkflowProgress) (string, error) {
	if !fallbackWhisper {
		progress.PauseForUserInput() // Clear spinner display before user prompt
		if !AskUser("Do you want to transcribe it using OpenAI's whisper ($$$)?") {
			return "", fmt.Errorf("transcription declined by user")
		}
	}

	// Download audio
	progress.UpdateStatus("Downloading audio...")
	audioFile, err := app.DownloadAudio(ctx, youtubeURL)
	if err != nil {
		return "", err
	}

	// Transcribe audio
	progress.UpdateStatus("Transcribing with OpenAI Whisper...")
	transcript, err := app.TranscribeAudio(ctx, audioFile)
	if err != nil {
		return "", err
	}

	// Save transcript
	_, youtubeID := ParseArg(youtubeURL)
	if err := SaveTranscript(youtubeID, transcript, app.config.TranscriptsDir); err != nil {
		progress.Log("Warning: %v\n", err)
	}

	return transcript, nil
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

// SummarizePlaylist summarizes all videos in a YouTube playlist
func (app *App) SummarizePlaylist(ctx context.Context, playlistURL string, fallbackWhisper bool) error {
	app.VerbosePrintf("Processing playlist...\n")

	// Get playlist information
	playlistInfo, err := app.youtube.PlaylistVideoURLs(ctx, playlistURL)
	if err != nil {
		return fmt.Errorf("extracting playlist videos: %w", err)
	}

	if len(playlistInfo.VideoURLs) == 0 {
		return fmt.Errorf("no videos found in playlist")
	}

	app.Printf("Found %d videos in playlist: %s\n\n", len(playlistInfo.VideoURLs), playlistInfo.Title)

	// Create progress bar - clean display without confusing rate for cached content
	bar := app.ui.NewProgressBar(len(playlistInfo.VideoURLs), "Gathering transcripts")

	// Collect all video transcripts
	var videoTranscripts []VideoTranscript
	var skippedVideos []string

	for i, videoURL := range playlistInfo.VideoURLs {
		bar.Set(i)

		// Check for existing cached transcript first (before expensive metadata fetch)
		_, youtubeID := ParseArg(videoURL)
		existingTranscriptPath := filepath.Join(app.config.TranscriptsDir, youtubeID+".txt")

		var transcript string
		var metadata *VideoMetadata

		if FileExists(existingTranscriptPath) {
			// Use cached transcript
			app.VerbosePrintf("\nUsing cached transcript for video %d\n", i+1)
			text, readErr := os.ReadFile(existingTranscriptPath)
			if readErr != nil {
				app.VerbosePrintf("Failed to read cached transcript: %v\n", readErr)
				skippedVideos = append(skippedVideos, fmt.Sprintf("Video %d (cache read error)", i+1))
				continue
			}
			transcript = string(text)

			// Try to load cached metadata
			cachedMetadata, err := LoadCachedMetadata(youtubeID, app.config.TranscriptsDir)
			if err != nil {
				app.VerbosePrintf("No cached metadata for video %d, fetching...\n", i+1)
				// Fetch and cache metadata
				metadata, err = app.Metadata(ctx, videoURL)
				if err != nil {
					app.VerbosePrintf("Failed to get metadata for video %d: %v\n", i+1, err)
					// Use placeholder metadata as fallback
					metadata = &VideoMetadata{
						Title:       fmt.Sprintf("Video %d", i+1),
						Channel:     "Unknown",
						Duration:    0,
						Description: "Metadata fetch failed",
					}
				} else {
					// Save metadata to cache for next time
					if err := SaveMetadata(youtubeID, metadata, app.config.TranscriptsDir); err != nil {
						app.VerbosePrintf("Warning: Failed to cache metadata: %v\n", err)
					}
				}
			} else {
				// Use cached metadata
				app.VerbosePrintf("Using cached metadata for video %d: %s\n", i+1, cachedMetadata.Title)
				metadata = cachedMetadata
			}
		} else {
			// Need to fetch transcript - get metadata first
			var err error
			metadata, err = app.Metadata(ctx, videoURL)
			if err != nil {
				app.VerbosePrintf("Failed to get metadata for video %d: %v\n", i+1, err)
				skippedVideos = append(skippedVideos, fmt.Sprintf("Video %d (metadata error)", i+1))
				continue
			}

			// Try to get transcript from YouTube
			transcript, err := app.GetTranscript(ctx, videoURL) //nolint:staticcheck,ineffassign // transcript is used later or reassigned in error case
			if err != nil {
				// If transcript fails and user wants fallback, ask per video
				if !fallbackWhisper {
					// Clear progress bar line before showing user prompt
					fmt.Print("\r\033[K")

					if !AskUser(fmt.Sprintf("Video %d (%s): '%s' has no captions. Use Whisper ($$$)?", i+1, youtubeID, metadata.Title)) {
						skippedVideos = append(skippedVideos, fmt.Sprintf("Video %d: %s", i+1, metadata.Title))
						continue
					}
				}

				// Try audio transcription
				audioFile, err := app.DownloadAudio(ctx, videoURL)
				if err != nil {
					app.VerbosePrintf("Failed to download audio for video %d: %v\n", i+1, err)
					skippedVideos = append(skippedVideos, fmt.Sprintf("Video %d: %s (audio error)", i+1, metadata.Title))
					continue
				}

				transcript, err = app.TranscribeAudio(ctx, audioFile)
				if err != nil {
					app.VerbosePrintf("Failed to transcribe audio for video %d: %v\n", i+1, err)
					skippedVideos = append(skippedVideos, fmt.Sprintf("Video %d: %s (transcription error)", i+1, metadata.Title))
					continue
				}

				// Save transcript for future use
				if err := SaveTranscript(youtubeID, transcript, app.config.TranscriptsDir); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				}
			}
		}

		// Truncate description if too long
		description := metadata.Description
		if len(description) > 150 {
			description = description[:147] + "..."
		}

		videoTranscripts = append(videoTranscripts, VideoTranscript{
			URL:         videoURL,
			Title:       metadata.Title,
			Channel:     metadata.Channel,
			Duration:    metadata.Duration,
			Description: description,
			Transcript:  transcript,
		})
	}

	bar.Finish()

	// Check if we have any transcripts to work with
	if len(videoTranscripts) == 0 {
		return fmt.Errorf("no video transcripts could be obtained")
	}

	// Report processing results
	app.Printf("Successfully processed %d out of %d videos\n", len(videoTranscripts), len(playlistInfo.VideoURLs))
	if len(skippedVideos) > 0 {
		app.Printf("Skipped %d videos:\n", len(skippedVideos))
		for _, skipped := range skippedVideos {
			app.Printf("  - %s\n", skipped)
		}
	}

	// Build combined transcript with structured format
	combinedTranscript := app.buildPlaylistTranscript(playlistInfo.Title, videoTranscripts)

	// Generate summary using the combined transcript - use single workflow spinner
	app.Printf("Generating playlist summary with OpenAI...\n")
	summary, err := app.GenerateSummary(ctx, playlistURL, combinedTranscript)
	if err != nil {
		return fmt.Errorf("generating playlist summary: %w", err)
	}

	app.PrintResult(summary)
	return nil
}

// buildPlaylistTranscript creates a structured transcript from all videos
func (app *App) buildPlaylistTranscript(playlistTitle string, videos []VideoTranscript) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Playlist: %s\n\n", playlistTitle))

	for i, video := range videos {
		// Format duration as minutes:seconds
		minutes := int(video.Duration / 60)
		seconds := int(video.Duration) % 60
		duration := fmt.Sprintf("%d:%02d", minutes, seconds)

		sb.WriteString(fmt.Sprintf("Video %d of %d: %s\n", i+1, len(videos), video.Title))
		sb.WriteString(fmt.Sprintf("Duration: %s | Channel: %s\n", duration, video.Channel))
		if video.Description != "" {
			sb.WriteString(fmt.Sprintf("Description: %s\n", video.Description))
		}
		sb.WriteString(video.Transcript)

		// Add separator between videos (except for the last one)
		if i < len(videos)-1 {
			sb.WriteString("\n\n---\n\n")
		}
	}

	return sb.String()
}
