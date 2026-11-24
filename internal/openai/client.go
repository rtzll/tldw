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
	"github.com/rtzll/tldw/internal/telemetry"
	"github.com/rtzll/tldw/internal/tldw"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type discardLogSink struct{}

func (discardLogSink) Printf(string, ...any) {}

type client interface {
	CreateTranscription(ctx context.Context, file *os.File) (string, error)
	CreateChatCompletion(ctx context.Context, model, prompt string) (string, error)
}

type sdkClient struct {
	client *openaisdk.Client
}

func newSDKClient(apiKey string) *sdkClient {
	client := openaisdk.NewClient(option.WithAPIKey(apiKey))
	return &sdkClient{client: &client}
}

func (c *sdkClient) CreateTranscription(ctx context.Context, file *os.File) (string, error) {
	resp, err := c.client.Audio.Transcriptions.New(ctx, openaisdk.AudioTranscriptionNewParams{
		File:  file,
		Model: openaisdk.AudioModelGPTTranscribe,
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func (c *sdkClient) CreateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
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
	getClient                func() (client, error)
	audio                    *Audio
	model                    string
	transcriptionUploadLimit int64
	timeout                  time.Duration
	verbose                  bool
	quiet                    bool
	log                      tldw.LogSink
}

// Config contains the validated operational settings for AI requests.
type Config struct {
	Model                    string
	TranscriptionUploadLimit int64
	Timeout                  time.Duration
	Verbose                  bool
	Quiet                    bool
}

// NewAIWithKey creates a new AI processor with lazy client initialization.
func NewAIWithKey(apiKey string, audio *Audio, config Config) (*AI, error) {
	if audio == nil {
		return nil, fmt.Errorf("audio processor is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if config.TranscriptionUploadLimit <= 0 {
		return nil, fmt.Errorf("transcription upload limit must be positive")
	}
	if config.Timeout < 0 {
		return nil, fmt.Errorf("timeout must not be negative")
	}
	return &AI{
		getClient: sync.OnceValues(func() (client, error) {
			if apiKey == "" {
				return nil, fmt.Errorf("OpenAI API key is required - set it in config.toml or OPENAI_API_KEY environment variable")
			}
			return newSDKClient(apiKey), nil
		}),
		audio:                    audio,
		model:                    config.Model,
		transcriptionUploadLimit: config.TranscriptionUploadLimit,
		timeout:                  config.Timeout,
		verbose:                  config.Verbose,
		quiet:                    config.Quiet,
		log:                      discardLogSink{},
	}, nil
}

func (ai *AI) SetLogSink(log tldw.LogSink) {
	ai.log = log
}

// Transcribe transcribes audio using the OpenAI audio transcriptions API.
func (ai *AI) Transcribe(ctx context.Context, audioFile string) (string, error) {
	api, err := ai.getClient()
	if err != nil {
		return "", err
	}

	if ai.verbose && !ai.quiet {
		ai.log.Printf("Transcribing audio file: %s\n", audioFile)
	}

	var prepared preparedAudio
	defer prepared.cleanup()
	prepareCtx, span := telemetry.Start(ctx, "audio.prepare")
	err = ai.fitAudio(prepareCtx, &prepared, audioFile, 0)
	span.SetAttributes(attribute.Int("tldw.audio.chunks", len(prepared.files)))
	tldw.EndOperation(span, err)
	if err != nil {
		return "", err
	}
	transcript, err := ai.processAudioChunks(ctx, api, prepared.files)
	if err != nil {
		return "", fmt.Errorf("transcribing audio: %w", err)
	}
	return transcript, nil
}

// files are upload inputs. Only workspaces created here belong to this request.
type preparedAudio struct {
	files      []string
	workspaces []chunkBatch
}

func (p *preparedAudio) cleanup() {
	for _, batch := range p.workspaces {
		batch.cleanup()
	}
}

// Prepare all chunks before the first paid upload. Descendants must fit or
// shrink, and upload order follows the original time intervals.
func (ai *AI) fitAudio(ctx context.Context, prepared *preparedAudio, path string, parentSize int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking audio chunk: %w", err)
	}
	size := info.Size()
	if size == 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("empty or invalid audio chunk %q", path)
	}
	if size <= ai.transcriptionUploadLimit {
		prepared.files = append(prepared.files, path)
		return nil
	}
	if parentSize > 0 && size >= parentSize {
		return fmt.Errorf("audio chunk cannot be split below upload limit")
	}
	count := 2
	if parentSize == 0 {
		count = int(1 + (size-1)/ai.transcriptionUploadLimit)
	}
	children, err := ai.audio.Split(ctx, path, count)
	if err != nil {
		return fmt.Errorf("splitting oversized audio: %w", err)
	}
	prepared.workspaces = append(prepared.workspaces, children)
	for _, child := range children.files {
		if err := ai.fitAudio(ctx, prepared, child, size); err != nil {
			return err
		}
	}
	return nil
}

func (ai *AI) processAudioChunks(ctx context.Context, api client, chunks []string) (string, error) {
	numChunks := len(chunks)

	if ai.verbose && !ai.quiet {
		ai.log.Printf("Transcribing chunks (%d)\n", numChunks)
	}

	var sb strings.Builder
	for i, chunkPath := range chunks {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		file, err := os.Open(chunkPath)
		if err != nil {
			return "", fmt.Errorf("opening chunk %s: %w", chunkPath, err)
		}

		uploadCtx, span := telemetry.Start(ctx, "openai.transcribe", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.Int("tldw.audio.chunk", i+1)))
		text, err := api.CreateTranscription(uploadCtx, file)
		tldw.EndOperation(span, err)
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
	api, err := ai.getClient()
	if err != nil {
		return "", err
	}

	if ai.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ai.timeout)
		defer cancel()
	}

	completionCtx, span := telemetry.Start(ctx, "openai.summary", trace.WithSpanKind(trace.SpanKindClient))
	content, err := api.CreateChatCompletion(completionCtx, ai.model, prompt)
	tldw.EndOperation(span, err)
	if err != nil {
		return "", fmt.Errorf("creating chat completion: %w", err)
	}

	return content, nil
}
