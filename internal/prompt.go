package internal

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// PromptData for template injection
type PromptData struct {
	Title       string
	Channel     string
	Description string
	Transcript  string
}

// PromptManager handles loading and processing prompt templates
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

	// Configure prompt based on config setting
	if promptSetting != "" {
		if IsLikelyFilePath(promptSetting) && FileExists(promptSetting) {
			pm.promptFile = promptSetting
		} else {
			pm.promptString = promptSetting
		}
	}

	return pm
}

// CreatePrompt builds a prompt from a transcript and metadata
func (pm *PromptManager) CreatePrompt(transcript string, metadata *VideoMetadata) (string, error) {
	var tmplContent string

	if pm.promptString != "" {
		// Use custom prompt string
		tmplContent = pm.promptString
	} else {
		// Use prompt file (custom or default from config directory)
		promptFile := pm.promptFile
		if promptFile == "" {
			// Use default prompt from config directory
			promptFile = filepath.Join(pm.configDir, "prompt.txt")
		}

		content, err := os.ReadFile(promptFile)
		if err != nil {
			return "", fmt.Errorf("reading prompt template: %w", err)
		}
		tmplContent = string(content)
	}

	return pm.buildPromptFromTemplate(tmplContent, transcript, metadata)
}

// buildPromptFromTemplate builds the AI prompt from template content
func (pm *PromptManager) buildPromptFromTemplate(templateContent, transcript string, metadata *VideoMetadata) (string, error) {
	// Parse the template
	tmpl, err := template.New("prompt").Parse(templateContent)
	if err != nil {
		return "", fmt.Errorf("parsing prompt template: %w", err)
	}

	// Prepare the data for the template
	data := PromptData{
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

// IsLikelyFilePath uses heuristics to determine if a string is likely a file path
func IsLikelyFilePath(s string) bool {
	// Check for common file path indicators
	if strings.Contains(s, "/") || strings.Contains(s, "\\") {
		return true
	}

	// Check for common file extensions
	if strings.Contains(s, ".txt") || strings.Contains(s, ".md") ||
		strings.Contains(s, ".template") || strings.Contains(s, ".tmpl") {
		return true
	}

	// If it's longer than 200 characters, it's likely a prompt string
	if len(s) > 200 {
		return false
	}

	// Default to treating as file path if it doesn't contain spaces and newlines
	return !strings.Contains(s, " ") && !strings.Contains(s, "\n")
}
