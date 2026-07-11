package internal

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/rtzll/tldw/internal/tldw"
)

type promptData struct {
	Title       string
	Channel     string
	Description string
	Transcript  string
}

// PromptManager handles loading and processing prompt templates.
type PromptManager struct {
	promptFile   string
	promptString string
	configDir    string
}

// NewPromptManager creates a new prompt manager
func NewPromptManager(configDir, promptSetting string) *PromptManager {
	pm := &PromptManager{
		configDir: configDir,
	}

	// Configure prompt based on config setting.
	if promptSetting != "" {
		if _, err := os.Stat(promptSetting); err == nil {
			pm.promptFile = promptSetting
		}
		if pm.promptFile == "" {
			pm.promptString = promptSetting
		}
	}

	return pm
}

// CreatePrompt builds a prompt from a transcript and metadata.
func (pm *PromptManager) CreatePrompt(transcript string, metadata *tldw.VideoMetadata) (string, error) {
	var tmplContent string

	if pm.promptString != "" {
		// Use custom prompt string.
		tmplContent = pm.promptString
	} else {
		// Use prompt file (custom or default from config directory).
		promptFile := pm.promptFile
		if promptFile == "" {
			// Use default prompt from config directory.
			promptFile = filepath.Join(pm.configDir, "prompt.txt")
		}

		content, err := os.ReadFile(promptFile)
		if err != nil {
			return "", fmt.Errorf("reading prompt template: %w", err)
		}
		tmplContent = string(content)
	}

	tmpl, err := template.New("prompt").Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("parsing prompt template: %w", err)
	}

	// Prepare the data for the template
	data := promptData{
		Transcript: transcript,
	}

	// Add metadata if available
	if metadata != nil {
		data.Title = metadata.Title
		data.Channel = metadata.Channel
		data.Description = metadata.Description
		// don't include chapters since it's likely part of the description
	}

	// Execute the template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing prompt template: %w", err)
	}

	return buf.String(), nil
}
