package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

func requestedTranscriptFormat(cmd *cobra.Command) internal.TranscriptRenderFormat {
	includeTimestamps, _ := cmd.Flags().GetBool("timestamps")
	if includeTimestamps {
		return internal.TranscriptRenderFormatTimestamps
	}

	return internal.TranscriptRenderFormatPlain
}

// fetchTranscript retrieves a transcript for the given argument and optionally falls back to Whisper.
func fetchTranscript(cmd *cobra.Command, app *internal.App, arg string) (string, error) {
	youtubeURL, _ := internal.ParseArg(arg)
	format := requestedTranscriptFormat(cmd)

	transcript, err := app.GetTranscriptOutput(cmd.Context(), youtubeURL, format)
	if err == nil {
		return transcript, nil
	}

	fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
	if !fallbackWhisper {
		return "", err
	}

	if format == internal.TranscriptRenderFormatTimestamps {
		return "", fmt.Errorf("timestamps are not supported with Whisper fallback yet")
	}

	audioFile, audioErr := app.DownloadAudio(cmd.Context(), youtubeURL)
	if audioErr != nil {
		return "", audioErr
	}

	structuredTranscript, whisperErr := app.TranscribeAudioStructured(cmd.Context(), audioFile)
	if whisperErr != nil {
		return "", whisperErr
	}

	_, youtubeID := internal.ParseArg(youtubeURL)
	structuredTranscript.VideoID = youtubeID

	transcript, renderErr := structuredTranscript.Render(internal.TranscriptRenderFormatPlain)
	if renderErr != nil {
		return "", renderErr
	}

	if saveErr := internal.SaveStructuredTranscript(structuredTranscript, config.TranscriptsDir); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", saveErr)
	}
	if saveErr := internal.SaveTranscript(youtubeID, transcript, config.TranscriptsDir); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", saveErr)
	}

	return transcript, nil
}
