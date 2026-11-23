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
	load func() (string, error)
}

// NewPromptManager resolves the source once. File-backed templates are read
// for each summary so edits take effect during a long-running process.
func NewPromptManager(configDir, setting string) *PromptManager {
	path := filepath.Join(configDir, "prompt.txt")
	if setting != "" {
		if _, err := os.Stat(setting); err != nil {
			return &PromptManager{load: func() (string, error) { return setting, nil }}
		}
		path = setting
	}
	return &PromptManager{load: func() (string, error) {
		data, err := os.ReadFile(path)
		return string(data), err
	}}
}

func (pm *PromptManager) CreatePrompt(transcript string, metadata *tldw.VideoMetadata) (string, error) {
	tmplContent, err := pm.load()
	if err != nil {
		return "", fmt.Errorf("reading prompt template: %w", err)
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
