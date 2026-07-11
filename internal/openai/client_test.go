package openai

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

const WhisperLimit int64 = 25 << 20

type mockOpenAIClient struct {
	transcription string
	chatResponse  string
	err           error
	checkContext  bool
}

func newTestAI(t *testing.T, client OpenAIClientInterface, audio *Audio, config Config) *AI {
	t.Helper()
	ai, err := NewAI(client, audio, config)
	if err != nil {
		t.Fatalf("NewAI() error = %v", err)
	}
	return ai
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

func TestAIEnsureClient(t *testing.T) {
	tests := []struct {
		name    string
		client  OpenAIClientInterface
		apiKey  string
		wantErr bool
	}{
		{"client already set", &mockOpenAIClient{}, "", false},
		{"no client no key", nil, "", true},
		{"lazy init with key", nil, "sk-test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ai := &AI{client: tt.client, apiKey: tt.apiKey}
			err := ai.ensureClient()
			if (err != nil) != tt.wantErr {
				t.Errorf("ensureClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAIEnsureClientConcurrent(t *testing.T) {
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai, err := NewAIWithKey("sk-test", audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit, Quiet: true})
	if err != nil {
		t.Fatalf("NewAIWithKey() error = %v", err)
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ai.ensureClient(); err != nil {
				t.Errorf("ensureClient() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if ai.client == nil {
		t.Fatal("expected lazy client to be initialized")
	}
}

func TestAIProcessAudioChunksWithProgress(t *testing.T) {
	client := &mockOpenAIClient{transcription: "Hello world"}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai := newTestAI(t, client, audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit})

	// Create temp files as chunks
	chunks := make([]string, 2)
	for i := range chunks {
		f, err := os.CreateTemp(t.TempDir(), "chunk*.mp3")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("closing temp chunk: %v", err)
		}
		chunks[i] = f.Name()
	}

	got, err := ai.processAudioChunks(context.Background(), chunks)
	if err != nil {
		t.Fatalf("processAudioChunksWithProgress() error = %v", err)
	}

	want := "Hello world\nHello world"
	if got != want {
		t.Errorf("processAudioChunksWithProgress() = %q, want %q", got, want)
	}
}

func TestAIProcessAudioChunksError(t *testing.T) {
	client := &mockOpenAIClient{err: fmt.Errorf("transcription failed")}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai := newTestAI(t, client, audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit})

	f, err := os.CreateTemp(t.TempDir(), "chunk*.mp3")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing temp chunk: %v", err)
	}

	_, err = ai.processAudioChunks(context.Background(), []string{f.Name()})
	if err == nil {
		t.Error("processAudioChunksWithProgress() expected error")
	}
}

func TestAISummary(t *testing.T) {
	client := &mockOpenAIClient{chatResponse: "A summary", checkContext: true}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai := newTestAI(t, client, audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit})

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
	ai := newTestAI(t, client, audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit})
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
	ai := newTestAI(t, client, audio, Config{Model: "gpt-5.4-mini", WhisperLimit: WhisperLimit})

	_, err := ai.Summary(context.Background(), "prompt")
	if err == nil {
		t.Error("Summary() expected error")
	}
}
