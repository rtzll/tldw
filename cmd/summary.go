package cmd

import (
	"context"
	"fmt"

	"github.com/rtzll/tldw/internal"
	"github.com/rtzll/tldw/internal/tldw"
)

type summaryProgress struct {
	bar     internal.Spinner
	verbose bool
}

func newSummaryProgress(config *internal.Config, description string) *summaryProgress {
	bar := internal.Spinner(internal.NoOpSpinner{})
	if !config.Quiet && !config.Verbose {
		bar = internal.NewSpinner(description)
	}
	return &summaryProgress{bar: bar, verbose: config.Verbose && !config.Quiet}
}

func (p *summaryProgress) update(description string) {
	p.bar.Describe(description)
	if p.verbose {
		fmt.Printf("[Status] %s\n", description)
	}
}

func (p *summaryProgress) finish() {
	p.bar.Finish()
}

func runSummary(ctx context.Context, engine *tldw.Engine, config *internal.Config, ref tldw.ContentRef, allowPaidTranscription bool) error {
	if ref.IsPlaylist() {
		return runPlaylistSummary(ctx, engine, config, ref, allowPaidTranscription)
	}

	progress := newSummaryProgress(config, "Processing video...")
	policy := tldw.CaptionsOnly
	if allowPaidTranscription {
		policy = tldw.CaptionsThenPaid
	}
	progress.update("Generating summary with OpenAI...")
	summary, err := engine.SummarizeVideo(ctx, ref, policy)
	if !allowPaidTranscription && tldw.CanOfferPaidTranscription(err) {
		progress.finish()
		if !askUser(fmt.Sprintf("%v. Use paid OpenAI transcription?", err)) {
			return fmt.Errorf("transcription declined by user")
		}
		progress = newSummaryProgress(config, "Transcribing with OpenAI...")
		summary, err = engine.SummarizeVideo(ctx, ref, tldw.PaidOnly)
	}
	if err != nil {
		progress.finish()
		return err
	}

	progress.update("Rendering summary...")
	rendered, err := renderMarkdown(summary.Markdown)
	progress.finish()
	if err != nil {
		return fmt.Errorf("rendering markdown: %w", err)
	}
	fmt.Println(rendered)
	return nil
}

func runPlaylistSummary(ctx context.Context, engine *tldw.Engine, config *internal.Config, ref tldw.ContentRef, allowPaidTranscription bool) error {
	request := tldw.PlaylistTranscripts(tldw.CaptionsOnly)
	if allowPaidTranscription {
		request = tldw.PlaylistTranscripts(tldw.CaptionsThenPaid)
	} else {
		request = tldw.PlaylistWithConsent(func(video tldw.ContentRef, metadata *tldw.VideoMetadata) bool {
			return askUser(fmt.Sprintf("Video %s: '%s' has no captions. Use paid OpenAI transcription?", video.ID(), metadata.Title))
		})
	}

	result, err := engine.CreatePlaylistSummary(ctx, ref, request)
	if err != nil {
		return err
	}
	if !config.Quiet {
		fmt.Printf("Found %d videos in playlist: %s\n\n", result.Total, result.Title)
		fmt.Printf("Successfully processed %d out of %d videos\n", result.Processed, result.Total)
		if len(result.Skipped) > 0 {
			fmt.Printf("Skipped %d videos:\n", len(result.Skipped))
			for _, skipped := range result.Skipped {
				fmt.Printf("  - %s\n", skipped)
			}
		}
	}
	rendered, err := renderMarkdown(result.Markdown)
	if err != nil {
		return fmt.Errorf("rendering markdown: %w", err)
	}
	fmt.Println(rendered)
	return nil
}
