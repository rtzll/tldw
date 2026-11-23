package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
	"github.com/rtzll/tldw/internal/tldw"
)

type summaryProgress struct {
	bar        internal.Spinner
	verbose    bool
	diagnostic io.Writer
}

func newSummaryProgress(config *internal.Config, description string, diagnostic io.Writer) *summaryProgress {
	bar := internal.Spinner(internal.NoOpSpinner{})
	if !config.Quiet && !config.Verbose {
		bar = internal.NewSpinner(description, diagnostic)
	}
	return &summaryProgress{bar: bar, diagnostic: diagnostic, verbose: config.Verbose && !config.Quiet}
}

func (p *summaryProgress) update(description string) {
	p.bar.Describe(description)
	if p.verbose {
		fmt.Fprintf(p.diagnostic, "[Status] %s\n", description)
	}
}

func (p *summaryProgress) finish() {
	p.bar.Finish()
}

func runSummary(cmd *cobra.Command, engine *tldw.Engine, config *internal.Config, ref tldw.ContentRef, allowPaidTranscription bool) error {
	ctx := cmd.Context()
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	input, diagnostic := cmd.InOrStdin(), cmd.ErrOrStderr()
	confirm := confirmationPrompt(ctx, input, diagnostic, canPrompt(input, diagnostic, nonInteractive))
	if ref.IsPlaylist() {
		return runPlaylistSummary(cmd, engine, config, ref, allowPaidTranscription, confirm)
	}

	summarize := func(policy tldw.TranscriptRequest, description string) (string, error) {
		progress := newSummaryProgress(config, description, diagnostic)
		defer progress.finish()
		progress.update("Generating summary with OpenAI...")
		summary, err := engine.SummarizeVideo(ctx, ref, policy)
		if err != nil {
			return "", err
		}
		progress.update("Rendering summary...")
		return summary.Markdown, nil
	}
	policy := tldw.CaptionsOnly
	if allowPaidTranscription {
		policy = tldw.CaptionsThenPaid
	}
	markdown, err := summarize(policy, "Processing video...")
	if !allowPaidTranscription && tldw.CanOfferPaidTranscription(err) {
		approved, confirmErr := confirm(fmt.Sprintf("%v. Use paid OpenAI transcription?", err))
		if confirmErr != nil {
			return confirmErr
		}
		if !approved {
			return errConsentDeclined
		}
		markdown, err = summarize(tldw.PaidOnly, "Transcribing with OpenAI...")
	}
	if err != nil {
		return err
	}
	return writeSummary(cmd.OutOrStdout(), markdown)
}

func runPlaylistSummary(cmd *cobra.Command, engine *tldw.Engine, config *internal.Config, ref tldw.ContentRef, allowPaidTranscription bool, confirm func(string) (bool, error)) error {
	var request tldw.PlaylistSummaryRequest
	if allowPaidTranscription {
		request = tldw.PlaylistTranscripts(tldw.CaptionsThenPaid)
	} else {
		request = tldw.PlaylistWithConsent(func(video tldw.ContentRef, metadata *tldw.VideoMetadata) (bool, error) {
			return confirm(fmt.Sprintf("Video %s: '%s' has no captions. Use paid OpenAI transcription?", video.ID(), metadata.Title))
		})
	}

	result, err := engine.CreatePlaylistSummary(cmd.Context(), ref, request)
	if (!config.Quiet && err == nil) || len(result.Skipped) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Found %d videos in playlist: %s\n\n", result.Total, result.Title)
		fmt.Fprintf(cmd.ErrOrStderr(), "Successfully processed %d out of %d videos\n", result.Processed, result.Total)
		if len(result.Skipped) > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Skipped %d videos:\n", len(result.Skipped))
			for _, skipped := range result.Skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "  - %s\n", skipped)
			}
		}
	}
	if err != nil {
		if result.Total > 0 && result.Declined == result.Total {
			return errConsentDeclined
		}
		return err
	}
	if err := writeSummary(cmd.OutOrStdout(), result.Markdown); err != nil {
		return err
	}
	if len(result.Skipped) > 0 {
		return errIncomplete
	}
	return nil
}
