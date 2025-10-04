package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

// fetchTranscript retrieves a transcript for the given argument and optionally falls back to Whisper.
func fetchTranscript(cmd *cobra.Command, app *internal.App, arg string) (string, error) {
	youtubeURL, _ := internal.ParseArg(arg)

	transcript, err := app.GetTranscript(cmd.Context(), youtubeURL)
	if err == nil {
		return transcript, nil
	}

	fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
	if !fallbackWhisper {
		return "", err
	}

	audioFile, audioErr := app.DownloadAudio(cmd.Context(), youtubeURL)
	if audioErr != nil {
		return "", audioErr
	}

	transcript, whisperErr := app.TranscribeAudio(cmd.Context(), audioFile)
	if whisperErr != nil {
		return "", whisperErr
	}

	_, youtubeID := internal.ParseArg(youtubeURL)
	if saveErr := internal.SaveTranscript(youtubeID, transcript, config.TranscriptsDir); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", saveErr)
	}

	return transcript, nil
}
