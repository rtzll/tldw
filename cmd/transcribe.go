package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"tldw/internal"
)

// transcribeCmd represents the transcribe command
var transcribeCmd = &cobra.Command{
	Use:   "transcribe [YouTube URL or ID]",
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
		youtubeURL, _ := internal.ParseArg(args[0])

		// Try to get transcript first
		transcript, err := app.GetTranscript(cmd.Context(), youtubeURL)
		if err != nil {
			// Check if fallback to Whisper is allowed
			fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
			if !fallbackWhisper {
				return err
			}

			// Download audio and transcribe with Whisper
			audioFile, err := app.DownloadAudio(cmd.Context(), youtubeURL)
			if err != nil {
				return err
			}

			transcript, err = app.TranscribeAudio(cmd.Context(), audioFile)
			if err != nil {
				return err
			}

			// Save transcript for future use
			_, youtubeID := internal.ParseArg(youtubeURL)
			if err := internal.SaveTranscript(youtubeID, transcript, config.TranscriptsDir); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			}
		}

		// Handle output flag
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
