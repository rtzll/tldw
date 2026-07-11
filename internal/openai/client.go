package openai

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/rtzll/tldw/internal/tldw"
)

type discardLogSink struct{}

func (discardLogSink) Printf(string, ...any) {}

// OpenAIClientInterface defines the interface for OpenAI client operations
type OpenAIClientInterface interface {
	CreateTranscription(ctx context.Context, file *os.File) (string, error)
	CreateChatCompletion(ctx context.Context, model, prompt string) (string, error)
}

// OpenAIClient wraps the official OpenAI Go SDK
type OpenAIClient struct {
	client *openaisdk.Client
}

// NewOpenAIClient creates a new OpenAI client
func NewOpenAIClient(apiKey string) *OpenAIClient {
	client := openaisdk.NewClient(option.WithAPIKey(apiKey))
	return &OpenAIClient{client: &client}
}

// CreateTranscription implements the transcription method
func (c *OpenAIClient) CreateTranscription(ctx context.Context, file *os.File) (string, error) {
	resp, err := c.client.Audio.Transcriptions.New(ctx, openaisdk.AudioTranscriptionNewParams{
		File:  file,
		Model: openaisdk.AudioModelWhisper1,
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

// CreateChatCompletion implements the chat completion method
func (c *OpenAIClient) CreateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
	oaiModel := openaisdk.ChatModel(model)

	resp, err := c.client.Chat.Completions.New(ctx, openaisdk.ChatCompletionNewParams{
		Model: oaiModel,
		Messages: []openaisdk.ChatCompletionMessageParamUnion{
			openaisdk.UserMessage(prompt),
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
	clientErr    error
	log          tldw.LogSink
}

// Config contains the validated operational settings for AI requests.
type Config struct {
	Model        string
	WhisperLimit int64
	Timeout      time.Duration
	Verbose      bool
	Quiet        bool
}

// NewAI creates a new AI processor
func NewAI(client OpenAIClientInterface, audio *Audio, config Config) (*AI, error) {
	if client == nil {
		return nil, fmt.Errorf("OpenAI client is required")
	}
	return newAI(client, "", audio, config)
}

// NewAIWithKey creates a new AI processor with lazy client initialization.
func NewAIWithKey(apiKey string, audio *Audio, config Config) (*AI, error) {
	return newAI(nil, apiKey, audio, config)
}

func newAI(client OpenAIClientInterface, apiKey string, audio *Audio, config Config) (*AI, error) {
	if audio == nil {
		return nil, fmt.Errorf("audio processor is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if config.WhisperLimit <= 0 {
		return nil, fmt.Errorf("whisper limit must be positive")
	}
	if config.Timeout < 0 {
		return nil, fmt.Errorf("timeout must not be negative")
	}
	return &AI{
		client:       client,
		audio:        audio,
		model:        config.Model,
		whisperLimit: config.WhisperLimit,
		timeout:      config.Timeout,
		verbose:      config.Verbose,
		quiet:        config.Quiet,
		apiKey:       apiKey,
		log:          discardLogSink{},
	}, nil
}

func (ai *AI) SetLogSink(log tldw.LogSink) {
	ai.log = log
}

// ensureClient initializes the OpenAI client if needed
func (ai *AI) ensureClient() error {
	ai.clientOnce.Do(func() {
		if ai.client != nil {
			return
		}
		if ai.apiKey == "" {
			ai.clientErr = validateAPIKey("")
			return
		}
		ai.client = NewOpenAIClient(ai.apiKey)
	})

	return ai.clientErr
}

func validateAPIKey(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("OpenAI API key is required - set it in config.toml or OPENAI_API_KEY environment variable")
	}
	return nil
}

// Transcribe transcribes audio using OpenAI's Whisper API
func (ai *AI) Transcribe(ctx context.Context, audioFile string) (string, error) {
	if err := ai.ensureClient(); err != nil {
		return "", err
	}

	if ai.verbose && !ai.quiet {
		ai.log.Printf("Transcribing audio file: %s\n", audioFile)
	}

	info, err := os.Stat(audioFile)
	if err != nil {
		return "", fmt.Errorf("getting audio file info: %w", err)
	}

	numChunks := int(info.Size() / ai.whisperLimit)
	if info.Size()%ai.whisperLimit != 0 {
		numChunks++
	}

	var chunks []string
	if numChunks > 1 {
		chunks, err = ai.audio.Split(ctx, audioFile, numChunks)
		if err != nil {
			return "", fmt.Errorf("splitting audio: %w", err)
		}
	} else {
		chunks = []string{audioFile}
	}

	if len(chunks) > 1 {
		defer cleanupFiles(chunks...)
	}

	transcript, err := ai.processAudioChunks(ctx, chunks)
	if err != nil {
		return "", fmt.Errorf("transcribing audio: %w", err)
	}
	return transcript, nil
}

func (ai *AI) processAudioChunks(ctx context.Context, chunks []string) (string, error) {
	numChunks := len(chunks)

	if ai.verbose && !ai.quiet {
		ai.log.Printf("Transcribing chunks (%d)\n", numChunks)
	}

	var sb strings.Builder
	for i, chunkPath := range chunks {
		file, err := os.Open(chunkPath)
		if err != nil {
			return "", fmt.Errorf("opening chunk %s: %w", chunkPath, err)
		}

		text, err := ai.client.CreateTranscription(ctx, file)
		if closeErr := file.Close(); closeErr != nil {
			ai.log.Printf("Warning: failed to close file %s: %v\n", chunkPath, closeErr)
		}
		if err != nil {
			return "", fmt.Errorf("transcribing chunk %d: %w", i+1, err)
		}

		sb.WriteString(text)
		if i < numChunks-1 {
			sb.WriteString("\n")
		}

		if ai.verbose && !ai.quiet {
			ai.log.Printf("Transcribed chunk %d/%d\n", i+1, numChunks)
		}
	}

	return sb.String(), nil
}

// Summary creates an AI summary using a prepared prompt
func (ai *AI) Summary(ctx context.Context, prompt string) (string, error) {
	if err := ai.ensureClient(); err != nil {
		return "", err
	}

	if ai.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ai.timeout)
		defer cancel()
	}

	content, err := ai.client.CreateChatCompletion(ctx, ai.model, prompt)
	if err != nil {
		return "", fmt.Errorf("creating chat completion: %w", err)
	}

	return content, nil
}
