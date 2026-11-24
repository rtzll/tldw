package cmd

import (
	"fmt"
	"github.com/rtzll/tldw/internal/tldw"

	"github.com/spf13/cobra"
)

// transcribeCmd represents the transcribe command
var transcribeCmd = &cobra.Command{
	Use:     "transcribe <video-id-or-url>",
	PreRunE: loadConfig,
	Short:   "Get a transcript from a YouTube video or single-video X post",
	Example: `  # Get transcript from YouTube captions
  tldw transcribe "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw transcribe tAP1eZYEuKA

  # Save transcript to file
  tldw transcribe tAP1eZYEuKA -o transcript.txt

  # Include timestamps when caption timing data is available
  tldw transcribe tAP1eZYEuKA --timestamps

  # Use paid OpenAI transcription if captions are unavailable
  tldw transcribe tAP1eZYEuKA --allow-paid-transcription`,
	Args: arguments(cobra.ExactArgs(1)),
	RunE: tracedCLI(func(cmd *cobra.Command, args []string) error {
		app, err := newEngine(config, cmd.ErrOrStderr())
		if err != nil {
			return fmt.Errorf("building application: %w", err)
		}

		ref, err := tldw.ParseVideoRef(args[0])
		if err != nil {
			return usageError{err}
		}
		return exportResult(cmd, func() ([]byte, error) {
			transcript, err := fetchTranscript(cmd, app, ref)
			return []byte(transcript), err
		})
	}),
}

func init() {
	addTranscriptionFlags(transcribeCmd)
	addOutputFlags(transcribeCmd)
	rootCmd.AddCommand(transcribeCmd)
}
