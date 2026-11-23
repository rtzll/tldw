package openai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type mockCommandRunner struct {
	output []byte
	err    error
	calls  []string
	args   [][]string
}

func (m *mockCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.calls = append(m.calls, name)
	m.args = append(m.args, args)
	return m.output, m.err
}

func TestAudioDuration(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    float64
		wantErr bool
	}{
		{"valid duration", "123.456\n", 123.456, false},
		{"with spaces", "  60.5  ", 60.5, false},
		{"zero", "0", 0, true},
		{"nonfinite", "NaN", 0, true},
		{"invalid output", "not-a-number", 0, true},
		{"command error", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &mockCommandRunner{
				output: []byte(tt.output),
			}
			if tt.wantErr && tt.output == "" && tt.name == "command error" {
				runner.err = fmt.Errorf("ffprobe failed")
			}

			a := NewAudio(runner, "/tmp")
			got, err := a.Duration(context.Background(), "test.mp3")
			if (err != nil) != tt.wantErr {
				t.Errorf("Audio.Duration() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Audio.Duration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAudioChunk(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		err     error
		wantErr bool
	}{
		{"success", "", nil, false},
		{"failure", "", fmt.Errorf("ffmpeg failed"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &mockCommandRunner{output: []byte(tt.output), err: tt.err}
			a := NewAudio(runner, t.TempDir())
			err := a.Chunk(context.Background(), "input.mp3", 10, 30, "output.mp3")
			if (err != nil) != tt.wantErr {
				t.Errorf("Audio.Chunk() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAudioSplit(t *testing.T) {
	runner := &mockCommandRunner{output: []byte("90.0\n")}
	tmpDir := t.TempDir()
	a := NewAudio(runner, tmpDir)

	chunks, err := a.Split(context.Background(), "input.mp3", 3)
	if err != nil {
		t.Fatalf("Audio.Split() error = %v", err)
	}

	if len(chunks.files) != 3 {
		t.Errorf("Audio.Split() returned %d chunks, want 3", len(chunks.files))
	}

	// Verify runner was called for duration and each chunk
	if len(runner.calls) != 4 {
		t.Errorf("Audio.Split() made %d calls, want 4", len(runner.calls))
	}
}

func TestAudioSplitDurationError(t *testing.T) {
	runner := &mockCommandRunner{err: fmt.Errorf("ffprobe failed")}
	a := NewAudio(runner, t.TempDir())

	_, err := a.Split(context.Background(), "input.mp3", 2)
	if err == nil {
		t.Error("Audio.Split() expected error when duration fails")
	}
}

func TestAudioSplitUsesPrivateWorkspaces(t *testing.T) {
	root := t.TempDir()
	audio := NewAudio(chunkingRunner{}, root)
	first, err := audio.Split(context.Background(), "same.mp3", 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := audio.Split(context.Background(), "same.mp3", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.cleanup()
	if filepath.Dir(first.files[0]) == filepath.Dir(second.files[0]) {
		t.Fatal("chunk directories shared")
	}
	first.cleanup()
	if _, err := os.Stat(second.files[0]); err != nil {
		t.Fatalf("cleanup removed another operation: %v", err)
	}
}

func TestShortAudioSplitsCoverItsDuration(t *testing.T) {
	runner := &mockCommandRunner{output: []byte("1.5")}
	audio := NewAudio(runner, t.TempDir())
	batch, err := audio.Split(t.Context(), "short.mp3", 3)
	if err != nil {
		t.Fatal(err)
	}
	defer batch.cleanup()
	for i, start := range []string{"0.000000", "0.500000", "1.000000"} {
		args := runner.args[i+1]
		if args[slices.Index(args, "-ss")+1] != start || args[slices.Index(args, "-t")+1] != "0.500000" {
			t.Fatalf("chunk %d: %v", i, args)
		}
	}
}
