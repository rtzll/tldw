package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// transcribeCmd represents the transcribe command
var transcribeCmd = &cobra.Command{
	Use:   "transcribe [URL]",
	Short: "Get a transcript from a YouTube video or single-video X post",
	Example: `  # Get transcript from YouTube captions
  tldw transcribe "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw transcribe tAP1eZYEuKA

  # Save transcript to file
  tldw transcribe tAP1eZYEuKA -o transcript.txt

  # Include timestamps when caption timing data is available
  tldw transcribe tAP1eZYEuKA --timestamps

  # Use paid OpenAI transcription if captions are unavailable
  tldw transcribe tAP1eZYEuKA --allow-paid-transcription`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newEngine(config)
		if err != nil {
			return fmt.Errorf("building application: %w", err)
		}

		transcript, err := fetchTranscript(cmd, app, args[0])
		if err != nil {
			return err
		}

		outputFile, _ := cmd.Flags().GetString("output")
		if outputFile != "" {
			return os.WriteFile(outputFile, []byte(transcript), 0644)
		}

		fmt.Println(transcript)
		return nil
	},
}

func init() {
	addTranscriptionFlags(transcribeCmd)
	transcribeCmd.Flags().StringP("output", "o", "", "Output file path (default: stdout)")
	rootCmd.AddCommand(transcribeCmd)
}
