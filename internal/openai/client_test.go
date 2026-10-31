package openai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const WhisperLimit int64 = 25 << 20

type mockOpenAIClient struct {
	transcription  string
	chatResponse   string
	chatModel      string
	err            error
	checkContext   bool
	transcriptions int
	uploadSizes    []int64
}

func TestNewAIRejectsInvalidConfiguration(t *testing.T) {
	audio := NewAudio(&mockCommandRunner{}, t.TempDir())
	valid := Config{Model: "gpt-6-luna", WhisperLimit: WhisperLimit}

	tests := []struct {
		name   string
		audio  *Audio
		config Config
	}{
		{name: "audio", config: valid},
		{name: "model", audio: audio, config: Config{WhisperLimit: WhisperLimit}},
		{name: "whisper limit", audio: audio, config: Config{Model: "gpt-6-luna"}},
		{name: "timeout", audio: audio, config: Config{Model: "gpt-6-luna", WhisperLimit: WhisperLimit, Timeout: -time.Second}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAIWithKey("test-key", test.audio, test.config); err == nil {
				t.Fatalf("NewAIWithKey() accepted invalid %s configuration", test.name)
			}
		})
	}
}

func (m *mockOpenAIClient) CreateTranscription(ctx context.Context, file *os.File) (string, error) {
	m.transcriptions++
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	m.uploadSizes = append(m.uploadSizes, info.Size())
	if m.checkContext && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return m.transcription, m.err
}

func (m *mockOpenAIClient) CreateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
	m.chatModel = model
	if m.checkContext && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return m.chatResponse, m.err
}

func TestAIWithKeyRequiresKeyWhenUsed(t *testing.T) {
	ai, err := NewAIWithKey("", NewAudio(&mockCommandRunner{}, t.TempDir()), Config{
		Model: "gpt-6-luna", WhisperLimit: WhisperLimit,
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
	return nil, os.WriteFile(args[len(args)-1], []byte("ok"), 0o644)
}

type oversizedChunkRunner struct{}

func (oversizedChunkRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "ffprobe" {
		return []byte("4\n"), nil
	}
	out := args[len(args)-1]
	data := []byte("xxx")
	if strings.Count(filepath.Base(out), "_chunk_") > 1 {
		data = []byte("x")
	}
	return nil, os.WriteFile(out, data, 0o644)
}

func TestAITranscribeResplitsOversizedChunks(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "audio.mp3")
	if err := os.WriteFile(input, []byte("four"), 0o600); err != nil {
		t.Fatal(err)
	}
	ai, err := NewAIWithKey("test", NewAudio(oversizedChunkRunner{}, dir), Config{Model: "test", WhisperLimit: 2})
	if err != nil {
		t.Fatal(err)
	}
	client := &mockOpenAIClient{transcription: "ok"}
	ai.client = client
	if _, err := ai.Transcribe(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(client.uploadSizes) != 4 {
		t.Fatalf("uploaded chunks = %v", client.uploadSizes)
	}
	for _, size := range client.uploadSizes {
		if size > 2 {
			t.Fatalf("oversized upload: %d", size)
		}
	}
}

func TestAITranscribeSplitsLargeAudio(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "audio.mp3")
	if err := os.WriteFile(input, []byte("four"), 0o644); err != nil {
		t.Fatalf("writing audio input: %v", err)
	}
	client := &mockOpenAIClient{transcription: "chunk transcript"}
	ai, err := NewAIWithKey("test-key", NewAudio(chunkingRunner{}, tempDir), Config{
		Model: "gpt-6-luna", WhisperLimit: 2,
	})
	if err != nil {
		t.Fatalf("NewAIWithKey() error = %v", err)
	}
	ai.client = client

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
	ai, err := NewAIWithKey("test-key", NewAudio(&mockCommandRunner{}, t.TempDir()), Config{
		Model: "gpt-6-luna", WhisperLimit: WhisperLimit,
	})
	if err != nil {
		t.Fatalf("NewAIWithKey() error = %v", err)
	}
	ai.client = &mockOpenAIClient{err: errors.New("transcription failed")}

	if _, err := ai.Transcribe(context.Background(), input); err == nil {
		t.Fatal("Transcribe() succeeded after client failure")
	}
}

func TestAISummary(t *testing.T) {
	client := &mockOpenAIClient{chatResponse: "A summary", checkContext: true}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir())
	ai, err := NewAIWithKey("test-key", audio, Config{Model: "gpt-6-luna", WhisperLimit: WhisperLimit})
	if err != nil {
		t.Fatalf("NewAIWithKey() error = %v", err)
	}
	ai.client = client

	got, err := ai.Summary(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if got != "A summary" {
		t.Errorf("Summary() = %q, want %q", got, "A summary")
	}
	if client.chatModel != "gpt-6-luna" {
		t.Errorf("summary model = %q, want gpt-6-luna", client.chatModel)
	}
}

func TestAITranscribePreservesCallerAudioFile(t *testing.T) {
	client := &mockOpenAIClient{transcription: "A transcript"}
	audio := NewAudio(&mockCommandRunner{}, t.TempDir())
	ai, err := NewAIWithKey("test-key", audio, Config{Model: "gpt-6-luna", WhisperLimit: WhisperLimit})
	if err != nil {
		t.Fatalf("NewAIWithKey() error = %v", err)
	}
	ai.client = client
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
	audio := NewAudio(runner, t.TempDir())
	ai, err := NewAIWithKey("test-key", audio, Config{Model: "gpt-6-luna", WhisperLimit: WhisperLimit})
	if err != nil {
		t.Fatalf("NewAIWithKey() error = %v", err)
	}
	ai.client = client

	_, err = ai.Summary(context.Background(), "prompt")
	if err == nil {
		t.Error("Summary() expected error")
	}
}
