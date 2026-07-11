package tldw_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestEngineWhisperOnlySkipsCaptionsAndReplacesCaptionCache(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.store.transcript = &tldw.Transcript{
		VideoID: testVideoID, Source: tldw.TranscriptSourceCaptions, Text: "cached captions",
	}

	got, err := fixture.engine.Transcript(context.Background(), videoRef(t), tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyWhisperOnly,
	})
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.Source != tldw.TranscriptSourceWhisper || got.PlainText() != "whisper transcript" {
		t.Fatalf("Transcript() = %+v", got)
	}
	if fixture.video.captionCalls != 0 || fixture.video.audioCalls != 1 || fixture.ai.transcribeCalls != 1 {
		t.Fatalf("calls: captions=%d audio=%d transcribe=%d", fixture.video.captionCalls, fixture.video.audioCalls, fixture.ai.transcribeCalls)
	}
	if fixture.store.transcriptSaves != 1 || fixture.store.transcript.Source != tldw.TranscriptSourceWhisper {
		t.Fatalf("saved transcript = %+v, saves = %d", fixture.store.transcript, fixture.store.transcriptSaves)
	}
}

func TestEngineRejectsUnknownTranscriptPolicyBeforeUsingDependencies(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})

	_, err := fixture.engine.Transcript(context.Background(), videoRef(t), tldw.TranscriptRequest{Policy: tldw.TranscriptPolicy(99)})
	if !errors.Is(err, tldw.ErrInvalidTranscriptPolicy) {
		t.Fatalf("Transcript() error = %v, want ErrInvalidTranscriptPolicy", err)
	}
	if fixture.video.metadataCalls != 0 || fixture.video.captionCalls != 0 || fixture.video.audioCalls != 0 {
		t.Fatalf("video dependency was used: %+v", fixture.video)
	}
}

func TestEngineDoesNotUseWhisperForTimestampRequest(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.video.metadata = &tldw.VideoMetadata{HasCaptions: false}

	_, err := fixture.engine.Transcript(context.Background(), videoRef(t), tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsThenWhisper, RequireTimestamps: true,
	})
	if !errors.Is(err, tldw.ErrTranscriptTimestampsUnavailable) {
		t.Fatalf("Transcript() error = %v, want ErrTranscriptTimestampsUnavailable", err)
	}
	if fixture.video.audioCalls != 0 || fixture.ai.transcribeCalls != 0 {
		t.Fatalf("Whisper path used: audio=%d transcribe=%d", fixture.video.audioCalls, fixture.ai.transcribeCalls)
	}
}

func TestEngineCaptionsOnlyRejectsCachedWhisper(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.store.transcript = &tldw.Transcript{VideoID: testVideoID, Source: tldw.TranscriptSourceWhisper, Text: "paid transcript"}
	fixture.video.metadata = &tldw.VideoMetadata{HasCaptions: false}

	_, err := fixture.engine.Transcript(context.Background(), videoRef(t), tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsOnly,
	})
	if !errors.Is(err, tldw.ErrCaptionsUnavailable) {
		t.Fatalf("Transcript() error = %v, want ErrCaptionsUnavailable", err)
	}
}

func TestEngineCaptionsOnlyDoesNotInventUnknownCacheSource(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.store.transcript = &tldw.Transcript{VideoID: testVideoID, Text: "legacy transcript"}
	fixture.video.captions = &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "verified captions"}

	got, err := fixture.engine.Transcript(context.Background(), videoRef(t), tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsOnly,
	})
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.PlainText() != "verified captions" || fixture.video.captionCalls != 1 {
		t.Fatalf("Transcript() = %q, caption calls = %d", got.PlainText(), fixture.video.captionCalls)
	}
}

func TestEngineCachesCaptionTranscript(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.video.captions = &tldw.Transcript{
		Source:   tldw.TranscriptSourceCaptions,
		Segments: []tldw.TranscriptSegment{{Start: 0, End: 2, Text: "hello world"}},
	}
	request := tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly}

	first, err := fixture.engine.Transcript(context.Background(), videoRef(t), request)
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	fixture.video.captionsErr = errors.New("caption source called after caching")
	second, err := fixture.engine.Transcript(context.Background(), videoRef(t), request)
	if err != nil {
		t.Fatalf("cached Transcript() error = %v", err)
	}
	if first.PlainText() != "hello world" || second.PlainText() != "hello world" {
		t.Fatalf("transcripts = %q, %q", first.PlainText(), second.PlainText())
	}
	if fixture.video.captionCalls != 1 || fixture.store.transcriptSaves != 1 {
		t.Fatalf("caption calls = %d, saves = %d", fixture.video.captionCalls, fixture.store.transcriptSaves)
	}
}

func TestEngineReturnsUnexpectedStoreFailure(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.store.transcriptErr = errors.New("cache is corrupt")

	_, err := fixture.engine.Transcript(context.Background(), videoRef(t), tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsOnly,
	})
	if err == nil || !errors.Is(err, fixture.store.transcriptErr) {
		t.Fatalf("Transcript() error = %v, want cache failure", err)
	}
	if fixture.video.captionCalls != 0 {
		t.Fatalf("caption calls = %d, want 0", fixture.video.captionCalls)
	}
}

func TestEngineFallsBackToWhisperWhenCaptionsAreUnavailable(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.video.metadata = &tldw.VideoMetadata{HasCaptions: false}

	got, err := fixture.engine.Transcript(context.Background(), videoRef(t), tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsThenWhisper,
	})
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.Source != tldw.TranscriptSourceWhisper || fixture.video.audioCalls != 1 || fixture.ai.transcribeCalls != 1 {
		t.Fatalf("Transcript() = %+v, audio=%d transcribe=%d", got, fixture.video.audioCalls, fixture.ai.transcribeCalls)
	}
}

func TestEngineSummarizeVideoReturnsTransportNeutralMarkdown(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.video.metadata = &tldw.VideoMetadata{Title: "Example", HasCaptions: true, CaptionLanguages: []string{"en"}}
	fixture.video.captions = &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "source transcript"}
	fixture.ai.summary = "## Raw summary"

	summary, err := fixture.engine.SummarizeVideo(context.Background(), videoRef(t), tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsOnly,
	})
	if err != nil {
		t.Fatalf("SummarizeVideo() error = %v", err)
	}
	if summary.Markdown != "## Raw summary" || fixture.prompts.transcript != "source transcript" {
		t.Fatalf("summary = %q, prompt transcript = %q", summary.Markdown, fixture.prompts.transcript)
	}
}

func TestEngineSummarizePlaylistReturnsTransportNeutralResult(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.video.metadata = &tldw.VideoMetadata{Title: "First", Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en"}}
	fixture.video.captions = &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "playlist transcript"}
	fixture.video.playlist = &tldw.PlaylistInfo{Title: "Examples", Videos: []tldw.YouTubeRef{videoRef(t)}}
	fixture.ai.summary = "## Playlist summary"

	result, err := fixture.engine.CreatePlaylistSummary(context.Background(), playlistRef(t), tldw.PlaylistSummaryRequest{
		Transcript: tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly},
	})
	if err != nil {
		t.Fatalf("CreatePlaylistSummary() error = %v", err)
	}
	if result.Markdown != "## Playlist summary" || result.Processed != 1 || result.Total != 1 {
		t.Fatalf("CreatePlaylistSummary() = %+v", result)
	}
}
