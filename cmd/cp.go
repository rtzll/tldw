package cmd

import (
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
)

// cpCmd copies the transcript to the system clipboard instead of printing to stdout.
var cpCmd = &cobra.Command{
	Use:   "cp [URL]",
	Short: "Copy a YouTube or X video transcript to the clipboard",
	Example: `  # Copy transcript from YouTube captions
  tldw cp "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw cp tAP1eZYEuKA

  # Use paid OpenAI transcription if captions are unavailable
  tldw cp tAP1eZYEuKA --allow-paid-transcription`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newEngine(config, cmd.ErrOrStderr())
		if err != nil {
			return fmt.Errorf("building application: %w", err)
		}

		transcript, err := fetchTranscript(cmd, app, args[0])
		if err != nil {
			return err
		}

		if err := clipboard.WriteAll(transcript); err != nil {
			return fmt.Errorf("copying transcript to clipboard: %w", err)
		}

		if !config.Quiet {
			fmt.Fprintln(cmd.ErrOrStderr(), "Transcript copied to clipboard")
		}

		return nil
	},
}

func init() {
	addTranscriptionFlags(cpCmd)
	rootCmd.AddCommand(cpCmd)
}
