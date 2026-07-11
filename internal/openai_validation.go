package internal

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/openai/openai-go/v3"
)

var modelNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,}$`)

func ValidateModel(model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("model cannot be empty")
	}
	if !modelNamePattern.MatchString(model) {
		return fmt.Errorf("invalid model format: %s (allowed: lowercase letters, digits, dot, underscore, hyphen)", model)
	}
	supported := []openai.ChatModel{
		openai.ChatModelO1, openai.ChatModelO1Mini, openai.ChatModelO3, openai.ChatModelO3Mini,
		openai.ChatModelO4Mini, openai.ChatModelGPT4o, openai.ChatModelGPT4oMini,
		openai.ChatModelGPT4_1, openai.ChatModelGPT4_1Mini, openai.ChatModelGPT4_1Nano,
		openai.ChatModelGPT5, openai.ChatModelGPT5_4Mini, openai.ChatModelGPT5Mini, openai.ChatModelGPT5Nano,
	}
	if slices.Contains(supported, openai.ChatModel(model)) {
		return nil
	}
	names := make([]string, 0, len(supported))
	for _, supportedModel := range supported {
		names = append(names, string(supportedModel))
	}
	return fmt.Errorf("unsupported model: %s (supported: %s)", model, strings.Join(names, ", "))
}

func ValidateOpenAIAPIKey(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("OpenAI API key is required - set it in config.toml or OPENAI_API_KEY environment variable")
	}
	return nil
}
