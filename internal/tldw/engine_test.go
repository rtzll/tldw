package tldw_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestEngineWhisperOnlySkipsCaptionsAndReplacesCaptionCache(t *testing.T) {
	store := &memoryStore{transcript: &tldw.Transcript{
		VideoID: testVideoID, Source: tldw.TranscriptSourceCaptions, Text: "cached captions",
	}}
	video := &videoStub{audioPath: "audio.mp3"}
	ai := &aiStub{transcription: "whisper transcript"}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: store, AI: ai, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	got, err := engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyWhisperOnly,
	})
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.Source != tldw.TranscriptSourceWhisper || got.PlainText() != "whisper transcript" {
		t.Fatalf("Transcript() = %+v", got)
	}
	if video.captionCalls != 0 || video.audioCalls != 1 || ai.transcribeCalls != 1 {
		t.Fatalf("calls: captions=%d audio=%d transcribe=%d", video.captionCalls, video.audioCalls, ai.transcribeCalls)
	}
	if store.transcriptSaves != 1 || store.transcript.Source != tldw.TranscriptSourceWhisper {
		t.Fatalf("saved transcript = %+v, saves = %d", store.transcript, store.transcriptSaves)
	}
}

func TestEngineRejectsUnknownTranscriptPolicyBeforeUsingDependencies(t *testing.T) {
	video := &videoStub{}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicy(99)})
	if !errors.Is(err, tldw.ErrInvalidTranscriptPolicy) {
		t.Fatalf("Transcript() error = %v, want ErrInvalidTranscriptPolicy", err)
	}
	if video.metadataCalls != 0 || video.captionCalls != 0 || video.audioCalls != 0 {
		t.Fatalf("video dependency was used: %+v", video)
	}
}

func TestEngineDoesNotUseWhisperForTimestampRequest(t *testing.T) {
	video := &videoStub{metadata: &tldw.VideoMetadata{HasCaptions: false}}
	ai := &aiStub{}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: ai, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsThenWhisper, RequireTimestamps: true,
	})
	if !errors.Is(err, tldw.ErrTranscriptTimestampsUnavailable) {
		t.Fatalf("Transcript() error = %v, want ErrTranscriptTimestampsUnavailable", err)
	}
	if video.audioCalls != 0 || ai.transcribeCalls != 0 {
		t.Fatalf("Whisper path used: audio=%d transcribe=%d", video.audioCalls, ai.transcribeCalls)
	}
}

func TestEngineCaptionsOnlyRejectsCachedWhisper(t *testing.T) {
	store := &memoryStore{transcript: &tldw.Transcript{VideoID: testVideoID, Source: tldw.TranscriptSourceWhisper, Text: "paid transcript"}}
	video := &videoStub{metadata: &tldw.VideoMetadata{HasCaptions: false}}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: store, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsOnly,
	})
	if !errors.Is(err, tldw.ErrCaptionsUnavailable) {
		t.Fatalf("Transcript() error = %v, want ErrCaptionsUnavailable", err)
	}
}

func TestEngineCaptionsOnlyDoesNotInventUnknownCacheSource(t *testing.T) {
	store := &memoryStore{transcript: &tldw.Transcript{VideoID: testVideoID, Text: "legacy transcript"}}
	video := &videoStub{
		metadata: &tldw.VideoMetadata{HasCaptions: true, CaptionLanguages: []string{"en"}},
		captions: &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "verified captions"},
	}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: store, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	got, err := engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsOnly,
	})
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.PlainText() != "verified captions" || video.captionCalls != 1 {
		t.Fatalf("Transcript() = %q, caption calls = %d", got.PlainText(), video.captionCalls)
	}
}

func TestEngineCachesCaptionTranscript(t *testing.T) {
	video := &videoStub{
		metadata: &tldw.VideoMetadata{HasCaptions: true, CaptionLanguages: []string{"en"}},
		captions: &tldw.Transcript{
			Source:   tldw.TranscriptSourceCaptions,
			Segments: []tldw.TranscriptSegment{{Start: 0, End: 2, Text: "hello world"}},
		},
	}
	store := &memoryStore{}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: store, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}
	request := tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly}

	first, err := engine.Transcript(context.Background(), ref, request)
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	video.captionsErr = errors.New("caption source called after caching")
	second, err := engine.Transcript(context.Background(), ref, request)
	if err != nil {
		t.Fatalf("cached Transcript() error = %v", err)
	}
	if first.PlainText() != "hello world" || second.PlainText() != "hello world" {
		t.Fatalf("transcripts = %q, %q", first.PlainText(), second.PlainText())
	}
	if video.captionCalls != 1 || store.transcriptSaves != 1 {
		t.Fatalf("caption calls = %d, saves = %d", video.captionCalls, store.transcriptSaves)
	}
}

func TestEngineReturnsUnexpectedStoreFailure(t *testing.T) {
	store := &memoryStore{transcriptErr: errors.New("cache is corrupt")}
	video := &videoStub{}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: store, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsOnly,
	})
	if err == nil || !errors.Is(err, store.transcriptErr) {
		t.Fatalf("Transcript() error = %v, want cache failure", err)
	}
	if video.captionCalls != 0 {
		t.Fatalf("caption calls = %d, want 0", video.captionCalls)
	}
}

func TestEngineFallsBackToWhisperWhenCaptionsAreUnavailable(t *testing.T) {
	video := &videoStub{metadata: &tldw.VideoMetadata{HasCaptions: false}, audioPath: "audio.mp3"}
	ai := &aiStub{transcription: "whisper transcript"}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: ai, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	got, err := engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsThenWhisper,
	})
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.Source != tldw.TranscriptSourceWhisper || video.audioCalls != 1 || ai.transcribeCalls != 1 {
		t.Fatalf("Transcript() = %+v, audio=%d transcribe=%d", got, video.audioCalls, ai.transcribeCalls)
	}
}

func TestEngineSummarizeVideoReturnsTransportNeutralMarkdown(t *testing.T) {
	video := &videoStub{
		metadata: &tldw.VideoMetadata{Title: "Example", HasCaptions: true, CaptionLanguages: []string{"en"}},
		captions: &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "source transcript"},
	}
	prompts := &promptStub{prompt: "prompt"}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: &aiStub{summary: "## Raw summary"}, Prompts: prompts,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	summary, err := engine.SummarizeVideo(context.Background(), ref, tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyCaptionsOnly,
	})
	if err != nil {
		t.Fatalf("SummarizeVideo() error = %v", err)
	}
	if summary.Markdown != "## Raw summary" || prompts.transcript != "source transcript" {
		t.Fatalf("summary = %q, prompt transcript = %q", summary.Markdown, prompts.transcript)
	}
}

func TestEngineSummarizePlaylistReturnsTransportNeutralResult(t *testing.T) {
	videoRef, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}
	playlistRef, err := tldw.ParseReference("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}
	video := &videoStub{
		metadata: &tldw.VideoMetadata{Title: "First", Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en"}},
		captions: &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "playlist transcript"},
		playlist: &tldw.PlaylistInfo{Title: "Examples", Videos: []tldw.YouTubeRef{videoRef}},
	}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: &aiStub{summary: "## Playlist summary"}, Prompts: &promptStub{prompt: "prompt"},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	result, err := engine.CreatePlaylistSummary(context.Background(), playlistRef, tldw.PlaylistSummaryRequest{
		Transcript: tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly},
	})
	if err != nil {
		t.Fatalf("CreatePlaylistSummary() error = %v", err)
	}
	if result.Markdown != "## Playlist summary" || result.Processed != 1 || result.Total != 1 {
		t.Fatalf("CreatePlaylistSummary() = %+v", result)
	}
}

func TestEngineSummarizePlaylistUsesWhisperAfterConsent(t *testing.T) {
	videoRef, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}
	playlistRef, err := tldw.ParseReference("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}
	video := &videoStub{
		playlist:  &tldw.PlaylistInfo{Title: "Examples", Videos: []tldw.YouTubeRef{videoRef}},
		metadata:  &tldw.VideoMetadata{Title: "No captions", Channel: "Channel", HasCaptions: false},
		audioPath: "audio.mp3",
	}
	ai := &aiStub{transcription: "whisper transcript", summary: "playlist summary"}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: ai, Prompts: &promptStub{prompt: "prompt"},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	confirmed := false

	result, err := engine.CreatePlaylistSummary(context.Background(), playlistRef, tldw.PlaylistSummaryRequest{
		Transcript: tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly},
		ConfirmWhisper: func(ref tldw.YouTubeRef, metadata *tldw.VideoMetadata) bool {
			confirmed = ref.ID() == testVideoID && metadata.Title == "No captions"
			return true
		},
	})
	if err != nil {
		t.Fatalf("CreatePlaylistSummary() error = %v", err)
	}
	if !confirmed || result.Processed != 1 || video.audioCalls != 1 || ai.transcribeCalls != 1 {
		t.Fatalf("confirmed=%v result=%+v audio=%d transcribe=%d", confirmed, result, video.audioCalls, ai.transcribeCalls)
	}
}
