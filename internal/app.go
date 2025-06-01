package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// App holds the application state and dependencies
type App struct {
	youtube       *YouTube
	audio         *Audio
	ai            *AI
	promptManager *PromptManager
	config        *Config
}

// NewApp initializes the application
func NewApp(config *Config, options ...AppOption) *App {
	cmdRunner := &DefaultCommandRunner{}

	// Create prompt manager
	promptManager := NewPromptManager(config.ConfigDir, config.Prompt)

	app := &App{
		youtube:       NewYouTube(os.DirFS("."), config.TranscriptsDir, config.Verbose),
		audio:         NewAudio(cmdRunner, config.TempDir, config.Verbose),
		ai:            NewAIWithKey(config.OpenAIAPIKey, nil, config.TLDRModel, WhisperLimit, config.SummaryTimeout, config.Verbose),
		promptManager: promptManager,
		config:        config,
	}

	// Set audio processor in AI processor after creation
	app.ai.audio = app.audio

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
	if err := EnsureDirs(app.config.CacheDir); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	audioFile, err := app.youtube.Audio(ctx, youtubeURL)
	if err != nil {
		return "", fmt.Errorf("downloading audio: %w", err)
	}

	return audioFile, nil
}

// TranscribeAudio transcribes an audio file and returns the transcript
func (app *App) TranscribeAudio(ctx context.Context, audioFile string) (string, error) {
	transcript, err := app.ai.Transcribe(ctx, audioFile)
	if err != nil {
		return "", err
	}

	return transcript, nil
}

// GetTranscript gets transcript from YouTube (cached or downloaded)
func (app *App) GetTranscript(ctx context.Context, youtubeURL string) (string, error) {
	if err := EnsureDirs(app.config.TranscriptsDir); err != nil {
		return "", fmt.Errorf("creating transcripts directory: %w", err)
	}

	_, youtubeID := ParseArg(youtubeURL)
	existingTranscriptPath := filepath.Join(app.config.TranscriptsDir, youtubeID+".txt")

	// Check for cached transcript
	if FileExists(existingTranscriptPath) {
		if app.config.Verbose {
			fmt.Printf("Found existing transcript for %s\n", youtubeID)
		}
		text, err := os.ReadFile(existingTranscriptPath)
		if err != nil {
			return "", fmt.Errorf("reading existing transcript: %w", err)
		}
		return string(text), nil
	}

	if app.config.Verbose {
		fmt.Printf("Fetching transcript for %s\n", youtubeID)
	}

	// Try to get transcript from YouTube
	transcript, err := app.youtube.FetchTranscript(ctx, youtubeURL)
	if err != nil || transcript == "" {
		return "", fmt.Errorf("no transcript available for %s", youtubeID)
	}

	// Save transcript for future use
	if err := SaveTranscript(youtubeID, transcript, app.config.TranscriptsDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	return transcript, nil
}

// GenerateSummary creates a summary from transcript and returns it
func (app *App) GenerateSummary(ctx context.Context, youtubeURL, transcript string) (string, error) {
	if transcript == "" {
		return "", fmt.Errorf("transcript is empty")
	}

	var metadata *VideoMetadata
	metadata, err := app.youtube.Metadata(ctx, youtubeURL)
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
	transcript, err := app.GetTranscript(ctx, youtubeURL)
	if err != nil {
		if !fallbackWhisper {
			if !AskUser("Do you want to transcribe it using OpenAI's whisper ($$$)?") {
				return fmt.Errorf("transcription declined by user")
			}
		}

		// Download audio and transcribe
		audioFile, err := app.DownloadAudio(ctx, youtubeURL)
		if err != nil {
			return err
		}

		transcript, err = app.TranscribeAudio(ctx, audioFile)
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
