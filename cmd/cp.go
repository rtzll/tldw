package cmd

import (
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

// cpCmd copies the transcript to the system clipboard instead of printing to stdout.
var cpCmd = &cobra.Command{
	Use:   "cp [URL]",
	Short: "Copy transcript from YouTube to the clipboard",
	Example: `  # Copy transcript from YouTube captions
  tldw cp "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw cp tAP1eZYEuKA

  # Use Whisper if no captions available (costs money)
  tldw cp tAP1eZYEuKA --fallback-whisper`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := internal.NewApp(config)

		transcript, err := fetchTranscript(cmd, app, args[0])
		if err != nil {
			return err
		}

		if err := clipboard.WriteAll(transcript); err != nil {
			return fmt.Errorf("copying transcript to clipboard: %w", err)
		}

		if !config.Quiet {
			fmt.Println("Transcript copied to clipboard")
		}

		return nil
	},
}

func init() {
	internal.AddTranscriptionFlags(cpCmd)
	rootCmd.AddCommand(cpCmd)
}
