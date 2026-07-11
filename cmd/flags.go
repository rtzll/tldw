package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

func addTranscriptionFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("fallback-whisper", false, "Fallback to Whisper if no captions available (costs money)")
	cmd.Flags().Bool("timestamps", false, "Include timestamps in transcript output when caption timing data is available")
}

func addOpenAIFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("model", "m", "", "OpenAI model to use for summaries")
	cmd.Flags().StringP("prompt", "p", "", "Custom prompt (string or file path)")
}

func handlePromptFlag(cmd *cobra.Command, config *internal.Config) error {
	promptFlag := cmd.Flags().Lookup("prompt")
	if promptFlag == nil || !promptFlag.Changed {
		return nil
	}

	prompt, err := cmd.Flags().GetString("prompt")
	if err != nil {
		return fmt.Errorf("failed to get prompt flag: %w", err)
	}
	if prompt != "" {
		config.Prompt = prompt
	}
	return nil
}

func applyOutputFlags(cmd *cobra.Command, config *internal.Config) error {
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return fmt.Errorf("failed to get verbose flag: %w", err)
	}
	quiet, err := cmd.Flags().GetBool("quiet")
	if err != nil {
		return fmt.Errorf("failed to get quiet flag: %w", err)
	}
	config.Verbose = verbose && !quiet
	config.Quiet = quiet
	return nil
}

func validateOpenAIRequirements(cmd *cobra.Command, config *internal.Config) error {
	if err := internal.ValidateOpenAIAPIKey(config.OpenAIAPIKey); err != nil {
		return err
	}

	modelFlag, err := cmd.Flags().GetString("model")
	if err != nil {
		return fmt.Errorf("failed to get model flag: %w", err)
	}
	if modelFlag != "" {
		if err := internal.ValidateModel(modelFlag); err != nil {
			return err
		}
		config.TLDRModel = modelFlag
		return nil
	}
	if err := internal.ValidateModel(config.TLDRModel); err != nil {
		return fmt.Errorf("invalid model in config: %w", err)
	}
	return nil
}
