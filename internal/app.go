package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
}

// NewApp initializes the application
func NewApp(config *Config, options ...AppOption) *App {
	cmdRunner := &DefaultCommandRunner{}

	promptManager := NewPromptManager(config.ConfigDir, config.Prompt)
	audio := NewAudio(cmdRunner, config.TempDir, config.Verbose)

	ui := NewUIManager(config.Verbose, config.Quiet)

	app := &App{
		youtube:       NewYouTube(os.DirFS("."), config.TranscriptsDir, config.Verbose),
		audio:         audio,
		ai:            NewAIWithKey(config.OpenAIAPIKey, audio, config.TLDRModel, WhisperLimit, config.SummaryTimeout, config.Verbose),
		promptManager: promptManager,
		config:        config,
		ui:            ui,
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
	return app.GetTranscriptWithStatus(ctx, youtubeURL, false)
}

// GetTranscriptWithStatus gets transcript with optional status spinner
func (app *App) GetTranscriptWithStatus(ctx context.Context, youtubeURL string, showStatus bool) (string, error) {
	var spinner ProgressBar
	if showStatus {
		spinner = app.ui.NewSpinner("Checking for existing captions...")
	}
	if err := EnsureDirs(app.config.TranscriptsDir); err != nil {
		if spinner != nil {
			spinner.Finish()
		}
		return "", fmt.Errorf("creating transcripts directory: %w", err)
	}

	_, youtubeID := ParseArg(youtubeURL)
	existingTranscriptPath := filepath.Join(app.config.TranscriptsDir, youtubeID+".txt")

	// Check for cached transcript
	if FileExists(existingTranscriptPath) {
		if spinner != nil {
			spinner.Describe("Found cached transcript")
			spinner.Finish()
		}
		if app.config.Verbose {
			fmt.Printf("Found existing transcript for %s\n", youtubeID)
		}
		text, err := os.ReadFile(existingTranscriptPath)
		if err != nil {
			return "", fmt.Errorf("reading existing transcript: %w", err)
		}
		return string(text), nil
	}

	if spinner != nil {
		spinner.Describe("Checking caption availability...")
		spinner.Advance()
	}

	// Check metadata first to see if captions are available (faster than attempting download)
	metadata, err := app.MetadataWithStatus(ctx, youtubeURL, false) // Don't show nested spinner
	if err != nil {
		if spinner != nil {
			spinner.Finish()
		}
		return "", fmt.Errorf("checking video metadata: %w", err)
	}

	// If no captions available, skip the expensive download attempt
	if !metadata.HasCaptions {
		if spinner != nil {
			spinner.Finish()
		}
		return "", fmt.Errorf("no captions available for %s", youtubeID)
	}

	if spinner != nil {
		spinner.Describe("Fetching YouTube captions...")
		spinner.Advance()
	}
	if app.config.Verbose {
		fmt.Printf("Fetching transcript for %s\n", youtubeID)
	}

	// Try to get transcript from YouTube (we know captions exist)
	transcript, err := app.youtube.FetchTranscript(ctx, youtubeURL)
	if err != nil || transcript == "" {
		// Only retry if it's a download failure (not other errors like invalid ID)
		if errors.Is(err, ErrDownloadFailed) {
			if spinner != nil {
				spinner.Describe("Download failed, retrying...")
			}
			if app.config.Verbose {
				fmt.Println("Download failed, retrying in 1 second...")
			}
			time.Sleep(1 * time.Second)

			transcript, err = app.youtube.FetchTranscript(ctx, youtubeURL)
		}

		if err != nil || transcript == "" {
			if spinner != nil {
				spinner.Finish()
			}
			return "", fmt.Errorf("no transcript available for %s", youtubeID)
		}
	}

	if spinner != nil {
		spinner.Describe("Saving transcript...")
		spinner.Advance()
	}

	// Save transcript for future use
	if err := SaveTranscript(youtubeID, transcript, app.config.TranscriptsDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	if spinner != nil {
		spinner.Finish()
	}
	return transcript, nil
}

// Metadata gets metadata from YouTube (cached or fresh)
func (app *App) Metadata(ctx context.Context, youtubeURL string) (*VideoMetadata, error) {
	return app.MetadataWithStatus(ctx, youtubeURL, false)
}

// MetadataWithStatus gets metadata with optional status spinner
func (app *App) MetadataWithStatus(ctx context.Context, youtubeURL string, showStatus bool) (*VideoMetadata, error) {
	var spinner ProgressBar
	if showStatus {
		spinner = app.ui.NewSpinner("Fetching video metadata...")
	}
	_, youtubeID := ParseArg(youtubeURL)

	// Try to load cached metadata first
	if cachedMetadata, err := LoadCachedMetadata(youtubeID, app.config.TranscriptsDir); err == nil {
		if spinner != nil {
			spinner.Describe("Found cached metadata")
			spinner.Finish()
		}
		if app.config.Verbose {
			fmt.Printf("Using cached metadata for %s\n", youtubeID)
		}
		return cachedMetadata, nil
	}

	// No cache found, fetch from YouTube
	if spinner != nil {
		spinner.Describe("Fetching video metadata from YouTube...")
		spinner.Advance()
	}
	if app.config.Verbose {
		fmt.Printf("Fetching fresh metadata for %s\n", youtubeID)
	}

	metadata, err := app.youtube.Metadata(ctx, youtubeURL)
	if err != nil {
		if spinner != nil {
			spinner.Finish()
		}
		return nil, err
	}

	// Cache the metadata for future use
	if spinner != nil {
		spinner.Describe("Caching metadata...")
		spinner.Advance()
	}
	if err := SaveMetadata(youtubeID, metadata, app.config.TranscriptsDir); err != nil {
		if app.config.Verbose {
			fmt.Printf("Warning: Failed to cache metadata: %v\n", err)
		}
	}

	if spinner != nil {
		spinner.Finish()
	}
	return metadata, nil
}

// GenerateSummary creates a summary from transcript and returns it
func (app *App) GenerateSummary(ctx context.Context, youtubeURL, transcript string) (string, error) {
	if transcript == "" {
		return "", fmt.Errorf("transcript is empty")
	}

	var metadata *VideoMetadata
	metadata, err := app.Metadata(ctx, youtubeURL)
	if err != nil {
		if app.config.Verbose {
			fmt.Printf("Failed to extract video metadata: %v\n", err)
		}
	}

	// Create the prompt using the PromptManager
	prompt, err := app.promptManager.CreatePrompt(transcript, metadata)
	if err != nil {
		return "", fmt.Errorf("creating prompt: %w", err)
	}

	// Get raw summary content from AI
	summaryContent, err := app.ai.Summary(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("generating summary: %w", err)
	}

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

	// Show status unless explicitly quiet
	showStatus := !app.config.Quiet
	transcript, err := app.GetTranscriptWithStatus(ctx, youtubeURL, showStatus)
	if err != nil {
		if !fallbackWhisper {
			if !AskUser("Do you want to transcribe it using OpenAI's whisper ($$$)?") {
				return fmt.Errorf("transcription declined by user")
			}
		}

		// Show status unless explicitly quiet (verbose is independent)
		showStatus := !app.config.Quiet
		transcript, err = app.transcribeVideoWithStatusSpinner(ctx, youtubeURL, showStatus)
		if err != nil {
			return err
		}

		// Save transcript
		_, youtubeID := ParseArg(youtubeURL)
		if err := SaveTranscript(youtubeID, transcript, app.config.TranscriptsDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
	}

	summary, err := app.GenerateSummary(ctx, youtubeURL, transcript)
	if err != nil {
		return err
	}

	fmt.Println(summary)
	return nil
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
	if app.config.Verbose {
		fmt.Println("Processing playlist...")
	}

	// Get playlist information
	playlistInfo, err := app.youtube.PlaylistVideoURLs(ctx, playlistURL)
	if err != nil {
		return fmt.Errorf("extracting playlist videos: %w", err)
	}

	if len(playlistInfo.VideoURLs) == 0 {
		return fmt.Errorf("no videos found in playlist")
	}

	app.ui.Printf("Found %d videos in playlist: %s\n\n", len(playlistInfo.VideoURLs), playlistInfo.Title)

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
			if app.config.Verbose {
				fmt.Printf("\nUsing cached transcript for video %d\n", i+1)
			}
			text, readErr := os.ReadFile(existingTranscriptPath)
			if readErr != nil {
				if app.config.Verbose {
					fmt.Printf("Failed to read cached transcript: %v\n", readErr)
				}
				skippedVideos = append(skippedVideos, fmt.Sprintf("Video %d (cache read error)", i+1))
				continue
			}
			transcript = string(text)

			// Try to load cached metadata
			cachedMetadata, err := LoadCachedMetadata(youtubeID, app.config.TranscriptsDir)
			if err != nil {
				if app.config.Verbose {
					fmt.Printf("No cached metadata for video %d, fetching...\n", i+1)
				}
				// Fetch and cache metadata
				metadata, err = app.Metadata(ctx, videoURL)
				if err != nil {
					if app.config.Verbose {
						fmt.Printf("Failed to get metadata for video %d: %v\n", i+1, err)
					}
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
						if app.config.Verbose {
							fmt.Printf("Warning: Failed to cache metadata: %v\n", err)
						}
					}
				}
			} else {
				// Use cached metadata
				if app.config.Verbose {
					fmt.Printf("Using cached metadata for video %d: %s\n", i+1, cachedMetadata.Title)
				}
				metadata = cachedMetadata
			}
		} else {
			// Need to fetch transcript - get metadata first
			var err error
			metadata, err = app.Metadata(ctx, videoURL)
			if err != nil {
				if app.config.Verbose {
					fmt.Printf("Failed to get metadata for video %d: %v\n", i+1, err)
				}
				skippedVideos = append(skippedVideos, fmt.Sprintf("Video %d (metadata error)", i+1))
				continue
			}

			// Try to get transcript from YouTube
			transcript, err := app.GetTranscript(ctx, videoURL)
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
					if app.config.Verbose {
						fmt.Printf("Failed to download audio for video %d: %v\n", i+1, err)
					}
					skippedVideos = append(skippedVideos, fmt.Sprintf("Video %d: %s (audio error)", i+1, metadata.Title))
					continue
				}

				transcript, err = app.TranscribeAudio(ctx, audioFile)
				if err != nil {
					if app.config.Verbose {
						fmt.Printf("Failed to transcribe audio for video %d: %v\n", i+1, err)
					}
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
	app.ui.Printf("Successfully processed %d out of %d videos\n", len(videoTranscripts), len(playlistInfo.VideoURLs))
	if len(skippedVideos) > 0 {
		app.ui.Printf("Skipped %d videos:\n", len(skippedVideos))
		for _, skipped := range skippedVideos {
			app.ui.Printf("  - %s\n", skipped)
		}
	}

	// Build combined transcript with structured format
	combinedTranscript := app.buildPlaylistTranscript(playlistInfo.Title, videoTranscripts)

	// Generate summary using the combined transcript
	summary, err := app.GenerateSummary(ctx, playlistURL, combinedTranscript)
	if err != nil {
		return fmt.Errorf("generating playlist summary: %w", err)
	}

	fmt.Println(summary)
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

// transcribeVideoWithStatusSpinner handles the complete transcription workflow with status spinners
func (app *App) transcribeVideoWithStatusSpinner(ctx context.Context, youtubeURL string, showStatus bool) (string, error) {
	if !showStatus || app.config.Verbose {
		// Fall back to non-status approach for verbose mode
		audioFile, err := app.DownloadAudio(ctx, youtubeURL)
		if err != nil {
			return "", err
		}
		return app.TranscribeAudio(ctx, audioFile)
	}

	// Stage 1: Download audio
	var spinner ProgressBar
	spinner = app.ui.NewSpinner("Downloading audio...")

	audioFile, err := app.DownloadAudio(ctx, youtubeURL)
	if err != nil {
		spinner.Finish()
		return "", err
	}

	// Stage 2: Transcription
	spinner.Describe("Transcribing with OpenAI Whisper...")
	spinner.Advance()

	transcript, err := app.TranscribeAudio(ctx, audioFile)
	if err != nil {
		spinner.Finish()
		return "", err
	}

	spinner.Describe("Transcription complete")
	spinner.Finish()
	return transcript, nil
}

// downloadAudioWithSharedProgress downloads audio and updates a shared progress bar within specified range
func (app *App) downloadAudioWithSharedProgress(ctx context.Context, youtubeURL string, bar ProgressBar, startPercent, endPercent int) (string, error) {
	if err := EnsureDirs(app.config.CacheDir); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	return app.youtube.AudioWithSharedProgress(ctx, youtubeURL, bar, startPercent, endPercent)
}

// transcribeAudioWithSharedProgress transcribes audio and updates a shared progress bar within specified range
func (app *App) transcribeAudioWithSharedProgress(ctx context.Context, audioFile string, bar ProgressBar, startPercent, endPercent int) (string, error) {
	return app.ai.TranscribeWithSharedProgress(ctx, audioFile, bar, startPercent, endPercent)
}
