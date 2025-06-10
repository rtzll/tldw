package internal

import (
	"fmt"

	"github.com/spf13/cobra"
)

// AddTranscriptionFlags adds flags related to transcription functionality
func AddTranscriptionFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("fallback-whisper", false, "Fallback to Whisper if no captions available (costs money)")
}

// AddOpenAIFlags adds flags related to OpenAI API functionality
func AddOpenAIFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("model", "m", "", "OpenAI model to use for summaries")
	cmd.Flags().StringP("prompt", "p", "", "Custom prompt (string or file path)")
}

// HandlePromptFlag processes the --prompt flag to set custom prompt
func HandlePromptFlag(cmd *cobra.Command, app *App) error {
	// Check if prompt flag was explicitly set
	promptFlag := cmd.Flags().Lookup("prompt")
	if promptFlag == nil || !promptFlag.Changed {
		return nil
	}

	prompt, err := cmd.Flags().GetString("prompt")
	if err != nil {
		return fmt.Errorf("failed to get prompt flag: %w", err)
	}

	// If prompt is empty, nothing to do
	if prompt == "" {
		return nil
	}

	// Create a new PromptManager with the specified prompt
	app.SetPromptManager(NewPromptManager(app.config.ConfigDir, prompt))

	// Check if it's a file path or a prompt string for verbose output
	if IsLikelyFilePath(prompt) && FileExists(prompt) {
		if app.config.Verbose {
			fmt.Printf("Using custom prompt file: %s\n", prompt)
		}
	} else {
		if app.config.Verbose {
			fmt.Printf("Using custom prompt string\n")
		}
	}

	return nil
}

// HandleVerboseFlag processes the --verbose flag to update config
func HandleVerboseFlag(cmd *cobra.Command, config *Config) error {
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return fmt.Errorf("failed to get verbose flag: %w", err)
	}
	config.Verbose = verbose
	return nil
}

// ValidateOpenAIRequirements validates OpenAI API key and model from command flags and config
func ValidateOpenAIRequirements(cmd *cobra.Command, config *Config) error {
	// Check OpenAI API key
	if err := ValidateOpenAIAPIKey(config.OpenAIAPIKey); err != nil {
		return err
	}

	// Handle model flag if provided
	modelFlag, _ := cmd.Flags().GetString("model")
	if modelFlag != "" {
		if err := ValidateModel(modelFlag); err != nil {
			return err
		}
		config.TLDRModel = modelFlag
	} else if err := ValidateModel(config.TLDRModel); err != nil {
		return fmt.Errorf("invalid model in config: %w", err)
	}

	return nil
}
