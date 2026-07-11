package openai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const WhisperLimit int64 = 25 << 20

type mockOpenAIClient struct {
	transcription  string
	chatResponse   string
	err            error
	checkContext   bool
	transcriptions int
}

func TestNewAIRejectsInvalidConfiguration(t *testing.T) {
	audio := NewAudio(&mockCommandRunner{}, t.TempDir(), false)
	valid := Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit}

	tests := []struct {
		name   string
		client OpenAIClientInterface
		audio  *Audio
		config Config
	}{
		{name: "client", audio: audio, config: valid},
		{name: "audio", client: &mockOpenAIClient{}, config: valid},
		{name: "model", client: &mockOpenAIClient{}, audio: audio, config: Config{WhisperLimit: WhisperLimit}},
		{name: "whisper limit", client: &mockOpenAIClient{}, audio: audio, config: Config{Model: "gpt-5.4-mini"}},
		{name: "timeout", client: &mockOpenAIClient{}, audio: audio, config: Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit, Timeout: -time.Second}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAI(test.client, test.audio, test.config); err == nil {
				t.Fatalf("NewAI() accepted invalid %s configuration", test.name)
			}
		})
	}
}

func (m *mockOpenAIClient) CreateTranscription(ctx context.Context, file *os.File) (string, error) {
	m.transcriptions++
	if m.checkContext && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return m.transcription, m.err
}

func (m *mockOpenAIClient) CreateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
	if m.checkContext && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return m.chatResponse, m.err
}

func TestAIWithKeyRequiresKeyWhenUsed(t *testing.T) {
	ai, err := NewAIWithKey("", NewAudio(&mockCommandRunner{}, t.TempDir(), false), Config{
		Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit,
	})
	if err != nil {
		t.Fatalf("NewAIWithKey() error = %v", err)
	}
	if _, err := ai.Summary(context.Background(), "prompt"); err == nil {
		t.Fatal("Summary() succeeded without an API key")
	}
}

type chunkingRunner struct{}

func (chunkingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "ffprobe" {
		return []byte("4\n"), nil
	}
	if name != "ffmpeg" || len(args) == 0 {
		return nil, fmt.Errorf("unexpected command: %s", name)
	}
	return nil, os.WriteFile(args[len(args)-1], []byte("chunk"), 0o644)
}

func TestAITranscribeSplitsLargeAudio(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "audio.mp3")
	if err := os.WriteFile(input, []byte("four"), 0o644); err != nil {
		t.Fatalf("writing audio input: %v", err)
	}
	client := &mockOpenAIClient{transcription: "chunk transcript"}
	ai, err := NewAI(client, NewAudio(chunkingRunner{}, tempDir, false), Config{
		Model: "gpt-5.4-mini", WhisperLimit: 2,
	})
	if err != nil {
		t.Fatalf("NewAI() error = %v", err)
	}

	got, err := ai.Transcribe(context.Background(), input)
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got != "chunk transcript\nchunk transcript" || client.transcriptions != 2 {
		t.Fatalf("Transcribe() = %q, calls = %d", got, client.transcriptions)
	}
}

func TestAITranscribeReturnsClientError(t *testing.T) {
	input := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(input, []byte("audio"), 0o644); err != nil {
		t.Fatalf("writing audio input: %v", err)
	}
	ai, err := NewAI(&mockOpenAIClient{err: errors.New("transcription failed")}, NewAudio(&mockCommandRunner{}, t.TempDir(), false), Config{
		Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit,
	})
	if err != nil {
		t.Fatalf("NewAI() error = %v", err)
	}

	if _, err := ai.Transcribe(context.Background(), input); err == nil {
		t.Fatal("Transcribe() succeeded after client failure")
	}
}

func TestAISummary(t *testing.T) {
	client := &mockOpenAIClient{chatResponse: "A summary", checkContext: true}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai, err := NewAI(client, audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit})
	if err != nil {
		t.Fatalf("NewAI() error = %v", err)
	}

	got, err := ai.Summary(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if got != "A summary" {
		t.Errorf("Summary() = %q, want %q", got, "A summary")
	}
}

func TestAITranscribePreservesCallerAudioFile(t *testing.T) {
	client := &mockOpenAIClient{transcription: "A transcript"}
	audio := NewAudio(&mockCommandRunner{}, t.TempDir(), false)
	ai, err := NewAI(client, audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit})
	if err != nil {
		t.Fatalf("NewAI() error = %v", err)
	}
	input := t.TempDir() + "/audio.mp3"
	if err := os.WriteFile(input, []byte("audio"), 0o644); err != nil {
		t.Fatalf("writing audio input: %v", err)
	}

	got, err := ai.Transcribe(context.Background(), input)
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got != "A transcript" {
		t.Fatalf("Transcribe() = %q, want A transcript", got)
	}
	if _, err := os.Stat(input); err != nil {
		t.Fatalf("Transcribe() removed caller audio: %v", err)
	}
}

func TestAISummaryError(t *testing.T) {
	client := &mockOpenAIClient{err: fmt.Errorf("API error")}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai, err := NewAI(client, audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit})
	if err != nil {
		t.Fatalf("NewAI() error = %v", err)
	}

	_, err = ai.Summary(context.Background(), "prompt")
	if err == nil {
		t.Error("Summary() expected error")
	}
}
