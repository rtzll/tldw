package tldw_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

func TestXPaidTranscriptionRequiresOptInAndSuccessfulVideoLookup(t *testing.T) {
	ref, err := tldw.ParseVideoRef("https://x.com/creatine_cycle/status/2123846497540489345")
	if err != nil {
		t.Fatal(err)
	}
	lookupErr := errors.New("post does not contain one accessible video")
	for _, tc := range []struct {
		name    string
		policy  tldw.TranscriptRequest
		video   videoStub
		wantErr error
	}{
		{"free", tldw.CaptionsOnly, videoStub{metadata: &tldw.VideoMetadata{}}, tldw.ErrCaptionsUnavailable},
		{"opted in", tldw.CaptionsThenPaid, videoStub{metadata: &tldw.VideoMetadata{}}, nil},
		{"dedicated paid tool", tldw.PaidOnly, videoStub{metadata: &tldw.VideoMetadata{}}, nil},
		{"invalid post", tldw.PaidOnly, videoStub{metadataErr: lookupErr}, lookupErr},
		{"failed lookup", tldw.CaptionsThenPaid, videoStub{metadataErr: lookupErr}, lookupErr},
		{"caption access failure", tldw.CaptionsThenPaid, videoStub{
			metadata: &tldw.VideoMetadata{HasCaptions: true}, captionsErr: lookupErr,
		}, lookupErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ai := &aiStub{transcription: "spoken text"}
			engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: &tc.video, Store: &memoryStore{}, AI: ai, Prompts: &promptStub{}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.Transcript(t.Context(), ref, tc.policy)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if tc.video.audioCalls != 0 || ai.transcribeCalls != 0 {
					t.Fatal("started paid transcription after an unsuccessful caption check")
				}
			} else if tc.video.audioCalls != 1 || ai.transcribeCalls != 1 || got.Source() != tldw.TranscriptSourcePaidTranscription || got.VideoID != ref.CacheKey() {
				t.Fatalf("incorrect paid transcription result %+v, audio calls %d, transcription calls %d", got, tc.video.audioCalls, ai.transcribeCalls)
			}
		})
	}
}

func TestEnginePaidTranscriptionOnlySkipsCaptionsAndPreservesCaptionCache(t *testing.T) {
	store := &memoryStore{transcripts: map[tldw.TranscriptSource]*tldw.Transcript{tldw.TranscriptSourceCaptions: fixtureCaptions(t, "cached captions")}}
	video := &videoStub{audioPath: "audio.mp3"}
	ai := &aiStub{transcription: "paid transcript"}
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

	got, err := engine.Transcript(context.Background(), ref, tldw.PaidOnly)
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.Source() != tldw.TranscriptSourcePaidTranscription || got.PlainText() != "paid transcript" {
		t.Fatalf("Transcript() = %+v", got)
	}
	if video.captionCalls != 0 || video.audioCalls != 1 || ai.transcribeCalls != 1 {
		t.Fatalf("calls: captions=%d audio=%d transcribe=%d", video.captionCalls, video.audioCalls, ai.transcribeCalls)
	}
	if store.transcriptSaves != 1 || len(store.transcripts) != 2 || store.transcripts[tldw.TranscriptSourcePaidTranscription] == nil {
		t.Fatalf("saved transcript = %+v, saves = %d", store.transcripts, store.transcriptSaves)
	}
}

func TestPaidAndCaptionTranscriptsSurviveAlternatingRequests(t *testing.T) {
	const captions = "direct captions"
	const paid = "paid transcription"
	for _, tc := range []struct {
		name      string
		oldRecord string
		wantPaid  int
		wantFetch int
	}{
		{"empty cache", "", 1, 1},
		{"existing paid", `{"source":"paid_transcription","text":"paid transcription"}`, 0, 1},
		{"existing whisper", `{"source":"whisper","text":"paid transcription"}`, 0, 1},
		{"existing captions", `{"source":"captions","cache_version":3,"caption_track":{"language":"en","direct":true},"segments":[{"start":1,"text":"direct captions"}]}`, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.oldRecord != "" {
				if err := os.WriteFile(filepath.Join(dir, testVideoID+".transcript.json"), []byte(tc.oldRecord), 0600); err != nil {
					t.Fatal(err)
				}
			}
			video := &videoStub{
				metadata: &tldw.VideoMetadata{Channel: "channel", HasCaptions: true, CaptionLanguages: []string{"en"}},
				captions: fixtureCaptions(t, "", tldw.TranscriptSegment{Start: 1, Text: captions}), audioPath: "audio.mp3",
			}
			ai := &aiStub{transcription: paid}
			ref, _ := tldw.ParseVideoRef(testVideoID)
			for _, step := range []struct {
				request tldw.TranscriptRequest
				text    string
			}{
				{tldw.PaidOnly, paid}, {tldw.CaptionsOnly, captions}, {tldw.PaidOnly, paid},
				{tldw.CaptionsThenPaid, captions}, {tldw.TimestampedCaptions, captions},
			} {
				// Each request starts a fresh engine, as separate CLI invocations do.
				engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
					Video: video, Store: store.NewFile(dir), AI: ai, Prompts: &promptStub{},
				})
				if err != nil {
					t.Fatal(err)
				}
				got, err := engine.Transcript(t.Context(), ref, step.request)
				if err != nil || got.PlainText() != step.text {
					t.Fatalf("request %v: got %v, error %v", step.request, got, err)
				}
			}
			if ai.transcribeCalls != tc.wantPaid || video.audioCalls != tc.wantPaid || video.captionCalls != tc.wantFetch {
				t.Fatalf("paid calls %d, audio downloads %d, caption fetches %d", ai.transcribeCalls, video.audioCalls, video.captionCalls)
			}
		})
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

	_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest(99))
	if !errors.Is(err, tldw.ErrInvalidTranscriptPolicy) {
		t.Fatalf("Transcript() error = %v, want ErrInvalidTranscriptPolicy", err)
	}
	if video.metadataCalls != 0 || video.captionCalls != 0 || video.audioCalls != 0 {
		t.Fatalf("video dependency was used: %+v", video)
	}
}

func TestEngineDoesNotUsePaidTranscriptionForTimestampRequest(t *testing.T) {
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

	_, err = engine.Transcript(context.Background(), ref, tldw.TimestampedTranscript)
	if !errors.Is(err, tldw.ErrTranscriptTimestampsUnavailable) {
		t.Fatalf("Transcript() error = %v, want ErrTranscriptTimestampsUnavailable", err)
	}
	if video.audioCalls != 0 || ai.transcribeCalls != 0 {
		t.Fatalf("paid transcription path used: audio=%d transcribe=%d", video.audioCalls, ai.transcribeCalls)
	}
}

func TestEngineCaptionsOnlyRejectsCachedPaidTranscription(t *testing.T) {
	paid, err := tldw.NewPaidTranscript(testVideoID, "paid transcript")
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{transcripts: map[tldw.TranscriptSource]*tldw.Transcript{tldw.TranscriptSourcePaidTranscription: paid}}
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

	_, err = engine.Transcript(context.Background(), ref, tldw.CaptionsOnly)
	if !errors.Is(err, tldw.ErrCaptionsUnavailable) {
		t.Fatalf("Transcript() error = %v, want ErrCaptionsUnavailable", err)
	}
}

func TestEngineCaptionsOnlyDoesNotInventUnknownCacheSource(t *testing.T) {
	legacy, err := tldw.NewLegacyTranscript(testVideoID, "legacy transcript", nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{transcripts: map[tldw.TranscriptSource]*tldw.Transcript{"": legacy}}
	video := &videoStub{
		metadata: &tldw.VideoMetadata{HasCaptions: true, CaptionLanguages: []string{"en"}},
		captions: fixtureCaptions(t, "verified captions"),
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

	got, err := engine.Transcript(context.Background(), ref, tldw.CaptionsOnly)
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
		captions: fixtureCaptions(t, "", tldw.TranscriptSegment{Start: 0, End: 2, Text: "hello world"}),
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
	request := tldw.CaptionsOnly

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

	_, err = engine.Transcript(context.Background(), ref, tldw.CaptionsOnly)
	if err == nil || !errors.Is(err, store.transcriptErr) {
		t.Fatalf("Transcript() error = %v, want cache failure", err)
	}
	if video.captionCalls != 0 {
		t.Fatalf("caption calls = %d, want 0", video.captionCalls)
	}
}

func TestEngineFallsBackToPaidTranscriptionWhenCaptionsAreUnavailable(t *testing.T) {
	video := &videoStub{metadata: &tldw.VideoMetadata{HasCaptions: false}, audioPath: "audio.mp3"}
	ai := &aiStub{transcription: "paid transcript"}
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

	got, err := engine.Transcript(context.Background(), ref, tldw.CaptionsThenPaid)
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.Source() != tldw.TranscriptSourcePaidTranscription || video.audioCalls != 1 || ai.transcribeCalls != 1 {
		t.Fatalf("Transcript() = %+v, audio=%d transcribe=%d", got, video.audioCalls, ai.transcribeCalls)
	}
}

func TestEngineDoesNotFallBackToPaidTranscriptionAfterCaptionCancellation(t *testing.T) {
	video := &videoStub{
		metadata:    &tldw.VideoMetadata{HasCaptions: true, CaptionLanguages: []string{"en"}},
		captionsErr: context.Canceled,
	}
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

	_, err = engine.Transcript(context.Background(), ref, tldw.CaptionsThenPaid)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Transcript() error = %v, want context.Canceled", err)
	}
	if video.audioCalls != 0 {
		t.Fatalf("paid fallback downloaded audio %d times after cancellation", video.audioCalls)
	}
}

func TestEngineSummarizeVideoReturnsTransportNeutralMarkdown(t *testing.T) {
	video := &videoStub{
		metadata: &tldw.VideoMetadata{Title: "Example", HasCaptions: true, CaptionLanguages: []string{"en"}},
		captions: fixtureCaptions(t, "source transcript"),
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

	summary, err := engine.SummarizeVideo(context.Background(), ref, tldw.CaptionsOnly)
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
		captions: fixtureCaptions(t, "playlist transcript"),
		playlist: &tldw.PlaylistInfo{Title: "Examples", Videos: []tldw.ContentRef{videoRef}},
	}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: &aiStub{summary: "## Playlist summary"}, Prompts: &promptStub{prompt: "prompt"},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	result, err := engine.CreatePlaylistSummary(context.Background(), playlistRef, tldw.PlaylistTranscripts(tldw.CaptionsOnly))
	if err != nil {
		t.Fatalf("CreatePlaylistSummary() error = %v", err)
	}
	if result.Markdown != "## Playlist summary" || result.Processed != 1 || result.Total != 1 {
		t.Fatalf("CreatePlaylistSummary() = %+v", result)
	}
}

func TestEngineSummarizePlaylistUsesPaidTranscriptionAfterConsent(t *testing.T) {
	videoRef, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}
	playlistRef, err := tldw.ParseReference("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}
	video := &videoStub{
		playlist:  &tldw.PlaylistInfo{Title: "Examples", Videos: []tldw.ContentRef{videoRef}},
		metadata:  &tldw.VideoMetadata{Title: "No captions", Channel: "Channel", HasCaptions: false},
		audioPath: "audio.mp3",
	}
	ai := &aiStub{transcription: "paid transcript", summary: "playlist summary"}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: ai, Prompts: &promptStub{prompt: "prompt"},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	confirmed := false

	result, err := engine.CreatePlaylistSummary(context.Background(), playlistRef, tldw.PlaylistWithConsent(func(ref tldw.ContentRef, metadata *tldw.VideoMetadata) bool {
		confirmed = ref.ID() == testVideoID && metadata.Title == "No captions"
		return true
	}))
	if err != nil {
		t.Fatalf("CreatePlaylistSummary() error = %v", err)
	}
	if !confirmed || result.Processed != 1 || video.audioCalls != 1 || ai.transcribeCalls != 1 {
		t.Fatalf("confirmed=%v result=%+v audio=%d transcribe=%d", confirmed, result, video.audioCalls, ai.transcribeCalls)
	}
}

func TestEngineDoesNotRetryOrSpendOnTerminalCaptionErrors(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, tldw.ErrRateLimited} {
		t.Run(cause.Error(), func(t *testing.T) {
			video := &videoStub{metadata: &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en"}}, captionsErr: fmt.Errorf("%w: %w", tldw.ErrDownloadFailed, cause)}
			ai := &aiStub{}
			engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: &memoryStore{}, AI: ai, Prompts: &promptStub{}})
			if err != nil {
				t.Fatal(err)
			}
			ref, _ := tldw.ParseVideoRef(testVideoID)
			_, err = engine.Transcript(context.Background(), ref, tldw.CaptionsThenPaid)
			if !errors.Is(err, cause) || video.captionCalls != 1 || video.audioCalls != 0 || ai.transcribeCalls != 0 {
				t.Fatalf("err=%v caption=%d audio=%d paid=%d", err, video.captionCalls, video.audioCalls, ai.transcribeCalls)
			}
		})
	}
}

func TestTimestampedPlaylistsSkipMissingCaptionsWithoutPaidWork(t *testing.T) {
	ref, _ := tldw.ParseVideoRef(testVideoID)
	playlist, _ := tldw.ParseReference("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	for _, intent := range []tldw.TranscriptRequest{tldw.TimestampedCaptions, tldw.TimestampedTranscript} {
		video := &videoStub{metadata: &tldw.VideoMetadata{Channel: "c"}, playlist: &tldw.PlaylistInfo{Videos: []tldw.ContentRef{ref}}}
		ai := &aiStub{}
		app, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: &memoryStore{}, AI: ai, Prompts: &promptStub{}})
		if err != nil {
			t.Fatal(err)
		}
		result, err := app.CreatePlaylistSummary(t.Context(), playlist, tldw.PlaylistTranscripts(intent))
		if err == nil || len(result.Skipped) != 1 || result.Processed != 0 || video.audioCalls != 0 || ai.transcribeCalls != 0 {
			t.Fatalf("intent=%v result=%+v err=%v audio=%d paid=%d", intent, result, err, video.audioCalls, ai.transcribeCalls)
		}
	}
}
