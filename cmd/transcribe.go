package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

// transcribeCmd represents the transcribe command
var transcribeCmd = &cobra.Command{
	Use:   "transcribe [URL]",
	Short: "Get transcript from YouTube (cached or downloaded)",
	Example: `  # Get transcript from YouTube captions
  tldw transcribe "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw transcribe tAP1eZYEuKA

  # Save transcript to file
  tldw transcribe tAP1eZYEuKA -o transcript.txt

  # Use Whisper if no captions available (costs money)
  tldw transcribe tAP1eZYEuKA --fallback-whisper`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := internal.NewApp(config)

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
	internal.AddTranscriptionFlags(transcribeCmd)
	transcribeCmd.Flags().StringP("output", "o", "", "Output file path (default: stdout)")
	rootCmd.AddCommand(transcribeCmd)
}
