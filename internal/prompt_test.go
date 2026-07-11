package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestPromptManagerCreatePrompt(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a default prompt file
	defaultPrompt := "Title: {{.Title}}\nTranscript: {{.Transcript}}"
	if err := os.WriteFile(filepath.Join(tmpDir, "prompt.txt"), []byte(defaultPrompt), 0644); err != nil {
		t.Fatalf("failed to write default prompt: %v", err)
	}

	t.Run("custom prompt string", func(t *testing.T) {
		pm := NewPromptManager(tmpDir, "Custom: {{.Transcript}}")
		got, err := pm.CreatePrompt("Hello world", nil)
		if err != nil {
			t.Fatalf("CreatePrompt() error = %v", err)
		}
		if got != "Custom: Hello world" {
			t.Errorf("CreatePrompt() = %q, want %q", got, "Custom: Hello world")
		}
	})

	t.Run("custom prompt file", func(t *testing.T) {
		customPath := filepath.Join(tmpDir, "custom prompt")
		if err := os.WriteFile(customPath, []byte("File: {{.Transcript}}"), 0644); err != nil {
			t.Fatalf("failed to write custom prompt file: %v", err)
		}

		pm := NewPromptManager(tmpDir, customPath)
		got, err := pm.CreatePrompt("Hello world", nil)
		if err != nil {
			t.Fatalf("CreatePrompt() error = %v", err)
		}
		if got != "File: Hello world" {
			t.Errorf("CreatePrompt() = %q, want %q", got, "File: Hello world")
		}
	})

	t.Run("default prompt file", func(t *testing.T) {
		pm := NewPromptManager(tmpDir, "")
		got, err := pm.CreatePrompt("Hello world", nil)
		if err != nil {
			t.Fatalf("CreatePrompt() error = %v", err)
		}
		if got != "Title: \nTranscript: Hello world" {
			t.Errorf("CreatePrompt() = %q, want %q", got, "Title: \nTranscript: Hello world")
		}
	})

	t.Run("with metadata", func(t *testing.T) {
		pm := NewPromptManager(tmpDir, "Title: {{.Title}}\nChannel: {{.Channel}}\nTranscript: {{.Transcript}}")
		metadata := &tldw.VideoMetadata{
			Title:   "Test Video",
			Channel: "Test Channel",
		}
		got, err := pm.CreatePrompt("Hello world", metadata)
		if err != nil {
			t.Fatalf("CreatePrompt() error = %v", err)
		}
		want := "Title: Test Video\nChannel: Test Channel\nTranscript: Hello world"
		if got != want {
			t.Errorf("CreatePrompt() = %q, want %q", got, want)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		pm := NewPromptManager(t.TempDir(), "")
		_, err := pm.CreatePrompt("Hello", nil)
		if err == nil {
			t.Error("CreatePrompt() expected error for missing file, got nil")
		}
	})

	t.Run("invalid template", func(t *testing.T) {
		pm := NewPromptManager(tmpDir, "{{.NonExistent}}")
		if _, err := pm.CreatePrompt("Hello", nil); err == nil {
			t.Fatal("CreatePrompt() accepted an invalid template")
		}
	})
}
