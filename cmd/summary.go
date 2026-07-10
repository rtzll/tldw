package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/rtzll/tldw/internal"
)

type summaryProgress struct {
	bar     internal.ProgressBar
	verbose bool
}

func newSummaryProgress(config *internal.Config, description string) *summaryProgress {
	bar := internal.ProgressBar(&internal.NoOpProgressBar{})
	if !config.Quiet && !config.Verbose {
		bar = internal.NewUIManager(config.Verbose, config.Quiet).NewSpinner(description)
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

func runSummary(ctx context.Context, engine *internal.Engine, config *internal.Config, ref internal.YouTubeRef, fallbackWhisper bool) error {
	if ref.ContentType == internal.ContentTypePlaylist {
		return runPlaylistSummary(ctx, engine, config, ref, fallbackWhisper)
	}

	progress := newSummaryProgress(config, "Processing video...")
	policy := internal.TranscriptPolicyCaptionsOnly
	if fallbackWhisper {
		policy = internal.TranscriptPolicyCaptionsThenWhisper
	}
	progress.update("Generating summary with OpenAI...")
	summary, err := engine.SummarizeVideo(ctx, ref, internal.TranscriptRequest{Policy: policy})
	if errors.Is(err, internal.ErrCaptionsUnavailable) && !fallbackWhisper {
		progress.finish()
		if !internal.AskUser("Do you want to transcribe it using OpenAI's whisper ($$$)?") {
			return fmt.Errorf("transcription declined by user")
		}
		progress = newSummaryProgress(config, "Transcribing with OpenAI Whisper...")
		summary, err = engine.SummarizeVideo(ctx, ref, internal.TranscriptRequest{Policy: internal.TranscriptPolicyWhisperOnly})
	}
	if err != nil {
		progress.finish()
		return err
	}

	progress.update("Rendering summary...")
	rendered, err := internal.RenderMarkdown(summary.Markdown)
	progress.finish()
	if err != nil {
		return fmt.Errorf("rendering markdown: %w", err)
	}
	fmt.Println(rendered)
	return nil
}

func runPlaylistSummary(ctx context.Context, engine *internal.Engine, config *internal.Config, ref internal.YouTubeRef, fallbackWhisper bool) error {
	request := internal.PlaylistSummaryRequest{
		Transcript: internal.TranscriptRequest{Policy: internal.TranscriptPolicyCaptionsOnly},
	}
	if fallbackWhisper {
		request.Transcript.Policy = internal.TranscriptPolicyCaptionsThenWhisper
	} else {
		request.ConfirmWhisper = func(video internal.YouTubeRef, metadata *internal.VideoMetadata) bool {
			return internal.AskUser(fmt.Sprintf("Video %s: '%s' has no captions. Use Whisper ($$$)?", video.ID, metadata.Title))
		}
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
	rendered, err := internal.RenderMarkdown(result.Markdown)
	if err != nil {
		return fmt.Errorf("rendering markdown: %w", err)
	}
	fmt.Println(rendered)
	return nil
}
