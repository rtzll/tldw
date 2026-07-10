package cmd

import (
	"errors"
	"fmt"

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
func fetchTranscript(cmd *cobra.Command, app *internal.Engine, arg string) (string, error) {
	parsed, err := internal.ParseVideoArg(arg)
	if err != nil {
		return "", err
	}
	format := requestedTranscriptFormat(cmd)

	fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
	if format == internal.TranscriptRenderFormatTimestamps {
		transcript, err := app.Transcript(cmd.Context(), parsed, internal.TranscriptRequest{
			Policy:            internal.TranscriptPolicyCaptionsOnly,
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

	policy := internal.TranscriptPolicyCaptionsOnly
	if fallbackWhisper {
		policy = internal.TranscriptPolicyCaptionsThenWhisper
	}
	transcript, err := app.Transcript(cmd.Context(), parsed, internal.TranscriptRequest{Policy: policy})
	if errors.Is(err, internal.ErrCaptionsUnavailable) && fallbackWhisper {
		transcript, err = app.Transcript(cmd.Context(), parsed, internal.TranscriptRequest{Policy: internal.TranscriptPolicyWhisperOnly})
	}
	if err != nil {
		return "", err
	}
	return transcript.Render(internal.TranscriptRenderFormatPlain)
}
