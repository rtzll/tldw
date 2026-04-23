package internal

import (
	"context"
	"fmt"
	"os"
	"testing"
)

type mockOpenAIClient struct {
	transcription string
	chatResponse  string
	err           error
}

func (m *mockOpenAIClient) CreateTranscription(ctx context.Context, file *os.File) (string, error) {
	return m.transcription, m.err
}

func (m *mockOpenAIClient) CreateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
	return m.chatResponse, m.err
}

func TestAIEnsureClient(t *testing.T) {
	tests := []struct {
		name      string
		client    OpenAIClientInterface
		apiKey    string
		wantErr   bool
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

func TestAIProcessAudioChunksWithProgress(t *testing.T) {
	client := &mockOpenAIClient{transcription: "Hello world"}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai := NewAI(client, audio, "gpt-5-nano", WhisperLimit, 0, false, false)

	// Create temp files as chunks
	chunks := make([]string, 2)
	for i := range chunks {
		f, err := os.CreateTemp(t.TempDir(), "chunk*.mp3")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		f.Close()
		chunks[i] = f.Name()
	}

	got, err := ai.processAudioChunksWithProgress(context.Background(), chunks, nil)
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
	ai := NewAI(client, audio, "gpt-5-nano", WhisperLimit, 0, false, false)

	f, err := os.CreateTemp(t.TempDir(), "chunk*.mp3")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	f.Close()

	_, err = ai.processAudioChunksWithProgress(context.Background(), []string{f.Name()}, nil)
	if err == nil {
		t.Error("processAudioChunksWithProgress() expected error")
	}
}

func TestAISummary(t *testing.T) {
	client := &mockOpenAIClient{chatResponse: "A summary"}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai := NewAI(client, audio, "gpt-5-nano", WhisperLimit, 0, false, false)

	got, err := ai.Summary(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if got != "A summary" {
		t.Errorf("Summary() = %q, want %q", got, "A summary")
	}
}

func TestAISummaryError(t *testing.T) {
	client := &mockOpenAIClient{err: fmt.Errorf("API error")}
	runner := &mockCommandRunner{}
	audio := NewAudio(runner, t.TempDir(), false)
	ai := NewAI(client, audio, "gpt-5-nano", WhisperLimit, 0, false, false)

	_, err := ai.Summary(context.Background(), "prompt")
	if err == nil {
		t.Error("Summary() expected error")
	}
}
