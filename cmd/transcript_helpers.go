package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal/tldw"
)

func requestedTranscriptFormat(cmd *cobra.Command) tldw.TranscriptRenderFormat {
	includeTimestamps, _ := cmd.Flags().GetBool("timestamps")
	if includeTimestamps {
		return tldw.TranscriptRenderFormatTimestamps
	}

	return tldw.TranscriptRenderFormatPlain
}

// fetchTranscript retrieves a transcript for the given argument and optionally falls back to Whisper.
func fetchTranscript(cmd *cobra.Command, app *tldw.Engine, arg string) (string, error) {
	parsed, err := tldw.ParseVideoRef(arg)
	if err != nil {
		return "", err
	}
	format := requestedTranscriptFormat(cmd)

	fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
	if format == tldw.TranscriptRenderFormatTimestamps {
		transcript, err := app.Transcript(cmd.Context(), parsed, tldw.TranscriptRequest{
			Policy:            tldw.TranscriptPolicyCaptionsOnly,
			RequireTimestamps: true,
		})
		if err != nil {
			if fallbackWhisper {
				return "", fmt.Errorf("timestamps are not supported with Whisper fallback yet")
			}
			return "", err
		}
		return transcript.Render(format)
	}

	policy := tldw.TranscriptPolicyCaptionsOnly
	if fallbackWhisper {
		policy = tldw.TranscriptPolicyCaptionsThenWhisper
	}
	transcript, err := app.Transcript(cmd.Context(), parsed, tldw.TranscriptRequest{Policy: policy})
	if errors.Is(err, tldw.ErrCaptionsUnavailable) && fallbackWhisper {
		transcript, err = app.Transcript(cmd.Context(), parsed, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyWhisperOnly})
	}
	if err != nil {
		return "", err
	}
	return transcript.Render(tldw.TranscriptRenderFormatPlain)
}
