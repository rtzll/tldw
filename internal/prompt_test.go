package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLikelyFilePath(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"unix path", "./prompts/custom.txt", true},
		{"windows path", `C:\Users\test\prompt.txt`, true},
		{"txt extension", "customprompt.txt", true},
		{"md extension", "customprompt.md", true},
		{"long string no spaces", "a"+string(make([]byte, 210)), false},
		{"with spaces", "This is a prompt string", false},
		{"with newline", "Line1\nLine2", false},
		{"short no indicators", "prompt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLikelyFilePath(tt.s); got != tt.want {
				t.Errorf("IsLikelyFilePath(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

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
		customPath := filepath.Join(tmpDir, "custom.txt")
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
		metadata := &VideoMetadata{
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
		pm := NewPromptManager(tmpDir, "")
		// Overwrite promptFile to non-existent path
		pm.promptFile = filepath.Join(tmpDir, "nonexistent.txt")
		pm.promptString = ""
		_, err := pm.CreatePrompt("Hello", nil)
		if err == nil {
			t.Error("CreatePrompt() expected error for missing file, got nil")
		}
	})
}

func TestPromptManagerBuildPromptFromTemplate(t *testing.T) {
	pm := &PromptManager{}

	tests := []struct {
		name     string
		template string
		transcript string
		metadata *VideoMetadata
		want     string
		wantErr  bool
	}{
		{
			name:       "simple template",
			template:   "Transcript: {{.Transcript}}",
			transcript: "Hello",
			metadata:   nil,
			want:       "Transcript: Hello",
			wantErr:    false,
		},
		{
			name:       "with metadata",
			template:   "Title: {{.Title}}\nTranscript: {{.Transcript}}",
			transcript: "Hello",
			metadata:   &VideoMetadata{Title: "Test"},
			want:       "Title: Test\nTranscript: Hello",
			wantErr:    false,
		},
		{
			name:       "invalid template",
			template:   "{{.NonExistent}}",
			transcript: "Hello",
			metadata:   nil,
			want:       "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pm.buildPromptFromTemplate(tt.template, tt.transcript, tt.metadata)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildPromptFromTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("buildPromptFromTemplate() = %q, want %q", got, tt.want)
			}
		})
	}
}
