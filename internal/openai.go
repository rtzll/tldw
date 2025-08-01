package internal

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAIClientInterface defines the interface for OpenAI client operations
type OpenAIClientInterface interface {
	CreateTranscription(ctx context.Context, file *os.File) (string, error)
	CreateChatCompletion(ctx context.Context, model, prompt string) (string, error)
}

// OpenAIClient wraps the official OpenAI Go SDK
type OpenAIClient struct {
	client *openai.Client
}

// NewOpenAIClient creates a new OpenAI client
func NewOpenAIClient(apiKey string) *OpenAIClient {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &OpenAIClient{client: &client}
}

// CreateTranscription implements the transcription method
func (c *OpenAIClient) CreateTranscription(ctx context.Context, file *os.File) (string, error) {
	resp, err := c.client.Audio.Transcriptions.New(ctx, openai.AudioTranscriptionNewParams{
		File:  file,
		Model: openai.AudioModelWhisper1,
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

// CreateChatCompletion implements the chat completion method
func (c *OpenAIClient) CreateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
	// Map model string to openai model constant
	var oaiModel openai.ChatModel
	switch model {
	case "gpt-4o":
		oaiModel = openai.ChatModelGPT4o
	case "gpt-4o-mini":
		oaiModel = openai.ChatModelGPT4oMini
	case "o4-mini":
		oaiModel = openai.ChatModelO4Mini
	case "gpt-4.1-nano":
		oaiModel = openai.ChatModelGPT4_1Nano
	default:
		return "", fmt.Errorf("unsupported model: %s", model)
	}

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: oaiModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices from OpenAI")
	}
	return resp.Choices[0].Message.Content, nil
}

// AI handles OpenAI API interactions for transcription and summarization
type AI struct {
	client       OpenAIClientInterface
	audio        *Audio
	model        string
	whisperLimit int64
	timeout      time.Duration
	verbose      bool
	quiet        bool
	apiKey       string
	clientOnce   sync.Once
}

// NewAI creates a new AI processor
func NewAI(client OpenAIClientInterface, audio *Audio, model string, whisperLimit int64, timeout time.Duration, verbose bool, quiet bool) *AI {
	return &AI{
		client:       client,
		audio:        audio,
		model:        model,
		whisperLimit: whisperLimit,
		timeout:      timeout,
		verbose:      verbose,
		quiet:        quiet,
	}
}

// NewAIWithKey creates a new AI processor with lazy client initialization
func NewAIWithKey(apiKey string, audio *Audio, model string, whisperLimit int64, timeout time.Duration, verbose bool, quiet bool) *AI {
	return &AI{
		client:       nil,
		audio:        audio,
		model:        model,
		whisperLimit: whisperLimit,
		timeout:      timeout,
		verbose:      verbose,
		quiet:        quiet,
		apiKey:       apiKey,
	}
}

// ensureClient initializes the OpenAI client if needed
func (ai *AI) ensureClient() error {
	if ai.client != nil {
		return nil
	}

	if ai.apiKey == "" {
		return ValidateOpenAIAPIKey("")
	}

	ai.clientOnce.Do(func() {
		ai.client = NewOpenAIClient(ai.apiKey)
	})

	return nil
}

// Transcribe transcribes audio using OpenAI's Whisper API
func (ai *AI) Transcribe(ctx context.Context, audioFile string) (string, error) {
	return ai.TranscribeWithProgress(ctx, audioFile, nil)
}

// TranscribeWithProgress transcribes audio with optional progress bar
// The progress bar should be created by the caller and passed in
func (ai *AI) TranscribeWithProgress(ctx context.Context, audioFile string, progressBar ProgressBar) (string, error) {
	if err := ai.ensureClient(); err != nil {
		return "", err
	}

	if ai.verbose && !ai.quiet {
		fmt.Printf("Transcribing audio file: %s\n", audioFile)
	}

	info, err := os.Stat(audioFile)
	if err != nil {
		return "", fmt.Errorf("getting audio file info: %w", err)
	}

	fileSize := info.Size()
	numChunks := int(math.Ceil(float64(fileSize) / float64(ai.whisperLimit)))

	var chunks []string
	if numChunks > 1 {
		chunks, err = ai.audio.Split(ctx, audioFile, numChunks)
		if err != nil {
			return "", fmt.Errorf("splitting audio: %w", err)
		}
	} else {
		chunks = []string{audioFile}
	}

	defer func() {
		cleanupFiles(chunks...)
		if len(chunks) > 1 {
			cleanupFiles(audioFile)
		}
	}()

	transcript, err := ai.processAudioChunksWithProgress(ctx, chunks, progressBar)
	if err != nil {
		return "", fmt.Errorf("transcribing audio: %w", err)
	}
	return transcript, nil
}

// processAudioChunks transcribes audio chunks sequentially
func (ai *AI) processAudioChunks(ctx context.Context, chunks []string) (string, error) {
	return ai.processAudioChunksWithProgress(ctx, chunks, nil)
}

// processAudioChunksWithProgress transcribes audio chunks with optional progress bar
// NOTE: tried to do it concurrently but one chunk returned broken transcript
// not use if issue with the invocation of the API or just a glitch
// trying it sequentially worked
func (ai *AI) processAudioChunksWithProgress(ctx context.Context, chunks []string, progressBar ProgressBar) (string, error) {
	numChunks := len(chunks)

	if ai.verbose && !ai.quiet {
		fmt.Printf("Transcribing chunks (%d)\n", numChunks)
	}

	// Progress bar should be created by UIManager and passed in
	// This method should not create UI elements directly

	var sb strings.Builder
	for i, chunkPath := range chunks {
		if progressBar != nil {
			progressBar.Set(i)
		}
		file, err := os.Open(chunkPath)
		if err != nil {
			return "", fmt.Errorf("opening chunk %s: %w", chunkPath, err)
		}

		text, err := ai.client.CreateTranscription(ctx, file)
		if closeErr := file.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close file %s: %v\n", chunkPath, closeErr)
		}
		if err != nil {
			return "", fmt.Errorf("transcribing chunk %d: %w", i+1, err)
		}

		sb.WriteString(text)
		if i < numChunks-1 {
			sb.WriteString("\n")
		}

		if ai.verbose && !ai.quiet {
			fmt.Printf("Transcribed chunk %d/%d\n", i+1, numChunks)
		}
	}

	// Complete progress bar
	if progressBar != nil {
		progressBar.Finish()
	}

	return sb.String(), nil
}

// Summary creates an AI summary using a prepared prompt
func (ai *AI) Summary(ctx context.Context, prompt string) (string, error) {
	if err := ai.ensureClient(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, ai.timeout)
	defer cancel()

	content, err := ai.client.CreateChatCompletion(ctx, ai.model, prompt)
	if err != nil {
		return "", fmt.Errorf("creating chat completion: %w", err)
	}

	return content, nil
}

// TranscribeWithSharedProgress transcribes audio with shared progress bar within specified range
func (ai *AI) TranscribeWithSharedProgress(ctx context.Context, audioFile string, bar ProgressBar, startPercent, endPercent int) (string, error) {
	if err := ai.ensureClient(); err != nil {
		return "", err
	}

	info, err := os.Stat(audioFile)
	if err != nil {
		return "", fmt.Errorf("getting audio file info: %w", err)
	}

	fileSize := info.Size()
	numChunks := int(math.Ceil(float64(fileSize) / float64(ai.whisperLimit)))

	var chunks []string
	if numChunks > 1 {
		chunks, err = ai.audio.Split(ctx, audioFile, numChunks)
		if err != nil {
			return "", fmt.Errorf("splitting audio: %w", err)
		}
	} else {
		chunks = []string{audioFile}
	}

	defer func() {
		cleanupFiles(chunks...)
		if len(chunks) > 1 {
			cleanupFiles(audioFile)
		}
	}()

	transcript, err := ai.processAudioChunksWithSharedProgress(ctx, chunks, bar, startPercent, endPercent)
	if err != nil {
		return "", fmt.Errorf("transcribing audio: %w", err)
	}
	return transcript, nil
}

// processAudioChunksWithSharedProgress transcribes audio chunks with shared progress bar within range
func (ai *AI) processAudioChunksWithSharedProgress(ctx context.Context, chunks []string, bar ProgressBar, startPercent, endPercent int) (string, error) {
	numChunks := len(chunks)
	progressRange := endPercent - startPercent

	var sb strings.Builder
	for i, chunkPath := range chunks {
		// Calculate progress within our allocated range
		chunkProgress := startPercent + (i * progressRange / numChunks)
		bar.Set(chunkProgress)

		file, err := os.Open(chunkPath)
		if err != nil {
			return "", fmt.Errorf("opening chunk %s: %w", chunkPath, err)
		}

		text, err := ai.client.CreateTranscription(ctx, file)
		if closeErr := file.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close file %s: %v\n", chunkPath, closeErr)
		}
		if err != nil {
			return "", fmt.Errorf("transcribing chunk %d: %w", i+1, err)
		}

		sb.WriteString(text)
		if i < numChunks-1 {
			sb.WriteString("\n")
		}
	}

	// Set final progress to end percent
	bar.Set(endPercent)
	return sb.String(), nil
}
