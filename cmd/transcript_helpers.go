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

// fetchTranscript retrieves a transcript for the given argument and optionally falls back to paid transcription.
func fetchTranscript(cmd *cobra.Command, app *tldw.Engine, arg string) (string, error) {
	parsed, err := tldw.ParseVideoRef(arg)
	if err != nil {
		return "", err
	}
	format := requestedTranscriptFormat(cmd)
	timestamps := format == tldw.TranscriptRenderFormatTimestamps
	fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
	policy := tldw.TranscriptPolicyCaptionsOnly
	if fallbackWhisper && !timestamps {
		policy = tldw.TranscriptPolicyCaptionsThenWhisper
	}
	transcript, err := app.Transcript(cmd.Context(), parsed, tldw.TranscriptRequest{Policy: policy, RequireTimestamps: timestamps})
	if err != nil {
		if errors.Is(err, tldw.ErrCaptionsUnavailable) {
			if timestamps {
				return "", fmt.Errorf("%w. For paid plain-text transcription, rerun with --fallback-whisper and omit --timestamps", err)
			}
			return "", fmt.Errorf("%w. For paid OpenAI transcription, rerun with --fallback-whisper", err)
		}
		if fallbackWhisper && errors.Is(err, tldw.ErrTranscriptTimestampsUnavailable) {
			return "", fmt.Errorf("%w. paid transcription produces plain text", err)
		}
		return "", err
	}
	return transcript.Render(format)
}
