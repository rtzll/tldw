package tldw_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

type captionTrackVideoStub struct {
	videoStub
	fetch func(*tldw.VideoMetadata) (*tldw.Transcript, error)
}

func (stub *captionTrackVideoStub) FetchCaptions(_ context.Context, _ tldw.YouTubeRef, metadata *tldw.VideoMetadata) (*tldw.Transcript, error) {
	stub.captionCalls++
	return stub.fetch(metadata)
}

func TestMissingTrackRefreshesOnceAndExcludesOnlyFailedSource(t *testing.T) {
	missing := tldw.CaptionTrack{Language: "en-orig", Automatic: true, Direct: true}
	manual := tldw.CaptionTrack{Language: "en", Direct: true}
	initial := &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en-orig"}, CaptionTracks: []tldw.CaptionTrack{missing}}
	fresh := &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en", "en-orig"}, CaptionTracks: []tldw.CaptionTrack{missing, manual}}
	video := &captionTrackVideoStub{videoStub: videoStub{metadata: fresh}}
	video.fetch = func(metadata *tldw.VideoMetadata) (*tldw.Transcript, error) {
		if video.captionCalls == 1 {
			return nil, &tldw.CaptionTrackUnavailableError{Track: missing}
		}
		if !reflect.DeepEqual(metadata.CaptionTracks, []tldw.CaptionTrack{manual}) {
			t.Fatalf("retry candidates = %+v", metadata.CaptionTracks)
		}
		return &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "Manual English", CaptionTrack: &manual}, nil
	}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: &memoryStore{metadata: initial}, AI: &aiStub{}, Prompts: &promptStub{}})
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := tldw.ParseVideoRef(testVideoID)
	got, err := engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly})
	if err != nil || got.Text != "Manual English" || video.metadataCalls != 1 || video.captionCalls != 2 {
		t.Fatalf("got %+v, err %v, metadata %d captions %d", got, err, video.metadataCalls, video.captionCalls)
	}
	if !reflect.DeepEqual(fresh.CaptionTracks, []tldw.CaptionTrack{missing, manual}) {
		t.Fatalf("shared metadata was changed: %+v", fresh)
	}
}

func TestMissingTrackRefreshFailureDoesNotRetryOrSpend(t *testing.T) {
	for _, cause := range []error{errors.New("lookup failed"), context.Canceled, context.DeadlineExceeded, tldw.ErrRateLimited} {
		t.Run(cause.Error(), func(t *testing.T) {
			missing := tldw.CaptionTrack{Language: "en-orig", Automatic: true, Direct: true}
			video := &captionTrackVideoStub{videoStub: videoStub{metadataErr: cause}}
			video.fetch = func(*tldw.VideoMetadata) (*tldw.Transcript, error) {
				return nil, &tldw.CaptionTrackUnavailableError{Track: missing}
			}
			ai := &aiStub{}
			cached := &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en-orig"}, CaptionTracks: []tldw.CaptionTrack{missing}}
			engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: &memoryStore{metadata: cached}, AI: ai, Prompts: &promptStub{}})
			if err != nil {
				t.Fatal(err)
			}
			ref, _ := tldw.ParseVideoRef(testVideoID)
			_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsThenWhisper})
			if !errors.Is(err, cause) || video.captionCalls != 1 || video.metadataCalls != 1 || video.audioCalls != 0 || ai.transcribeCalls != 0 {
				t.Fatalf("err %v, metadata %d captions %d audio %d paid %d", err, video.metadataCalls, video.captionCalls, video.audioCalls, ai.transcribeCalls)
			}
		})
	}
}

func TestMissingTracksDoNotLoopAfterRefresh(t *testing.T) {
	track := tldw.CaptionTrack{Language: "en-orig", Automatic: true, Direct: true}
	metadata := &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en-orig"}, CaptionTracks: []tldw.CaptionTrack{track}}
	video := &captionTrackVideoStub{videoStub: videoStub{metadata: metadata}}
	video.fetch = func(*tldw.VideoMetadata) (*tldw.Transcript, error) { return nil, tldw.ErrCaptionsUnavailable }
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: &memoryStore{metadata: metadata}, AI: &aiStub{}, Prompts: &promptStub{}})
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := tldw.ParseVideoRef(testVideoID)
	_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly})
	if !errors.Is(err, tldw.ErrCaptionsUnavailable) || video.metadataCalls != 1 || video.captionCalls != 2 {
		t.Fatalf("err %v, metadata %d captions %d", err, video.metadataCalls, video.captionCalls)
	}
}

func TestMissingTrackRetryTerminalErrorDoesNotSpend(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, tldw.ErrRateLimited} {
		t.Run(cause.Error(), func(t *testing.T) {
			missing := tldw.CaptionTrack{Language: "en-orig", Automatic: true, Direct: true}
			manual := tldw.CaptionTrack{Language: "en", Direct: true}
			metadata := &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en", "en-orig"}, CaptionTracks: []tldw.CaptionTrack{missing, manual}}
			video := &captionTrackVideoStub{videoStub: videoStub{metadata: metadata}}
			video.fetch = func(*tldw.VideoMetadata) (*tldw.Transcript, error) {
				if video.captionCalls == 1 {
					return nil, &tldw.CaptionTrackUnavailableError{Track: missing}
				}
				return nil, cause
			}
			ai := &aiStub{}
			engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: &memoryStore{metadata: metadata}, AI: ai, Prompts: &promptStub{}})
			if err != nil {
				t.Fatal(err)
			}
			ref, _ := tldw.ParseVideoRef(testVideoID)
			_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsThenWhisper})
			if !errors.Is(err, cause) || video.captionCalls != 2 || video.metadataCalls != 1 || video.audioCalls != 0 || ai.transcribeCalls != 0 {
				t.Fatalf("err %v, metadata %d captions %d audio %d paid %d", err, video.metadataCalls, video.captionCalls, video.audioCalls, ai.transcribeCalls)
			}
		})
	}
}

func TestLegacyCaptionCacheSurvivesRateLimit(t *testing.T) {
	dir := t.TempDir()
	old := map[string]string{
		".transcript.json": `{"cache_version":2,"source":"captions","text":"Old translated captions"}`,
		".txt":             "Old translated captions",
	}
	for suffix, contents := range old {
		if err := os.WriteFile(filepath.Join(dir, testVideoID+suffix), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	track := tldw.CaptionTrack{Language: "en-orig", Automatic: true, Direct: true}
	metadata := &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en-orig"}, CaptionTracks: []tldw.CaptionTrack{track}}
	adapter := store.NewFile(dir)
	if err := adapter.SaveMetadata(testVideoID, metadata); err != nil {
		t.Fatal(err)
	}
	video := &captionTrackVideoStub{}
	video.fetch = func(*tldw.VideoMetadata) (*tldw.Transcript, error) { return nil, tldw.ErrRateLimited }
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: adapter, AI: &aiStub{}, Prompts: &promptStub{}})
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := tldw.ParseVideoRef(testVideoID)
	_, err = engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly})
	if !errors.Is(err, tldw.ErrRateLimited) || video.captionCalls != 1 || video.metadataCalls != 0 {
		t.Fatalf("err %v, metadata %d captions %d", err, video.metadataCalls, video.captionCalls)
	}
	for suffix, contents := range old {
		retained, err := os.ReadFile(filepath.Join(dir, testVideoID+suffix))
		if err != nil || string(retained) != contents {
			t.Fatalf("failed migration changed %s: %v", suffix, err)
		}
	}
}

func TestLegacyPositiveMetadataAndCaptionsAreReacquired(t *testing.T) {
	dir := t.TempDir()
	old := map[string]string{
		".meta.json":       `{"cache_version":3,"channel":"Channel","has_captions":true,"caption_languages":["en"],"cached_at":"2025-12-21T12:00:00Z"}`,
		".transcript.json": `{"cache_version":2,"source":"captions","text":"Old translated captions"}`,
		".txt":             "Old translated captions",
	}
	for suffix, contents := range old {
		if err := os.WriteFile(filepath.Join(dir, testVideoID+suffix), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	track := tldw.CaptionTrack{Language: "en-orig", Automatic: true, Direct: true}
	metadata := &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en", "en-orig"}, CaptionTracks: []tldw.CaptionTrack{track}}
	video := &captionTrackVideoStub{videoStub: videoStub{metadata: metadata}}
	video.fetch = func(got *tldw.VideoMetadata) (*tldw.Transcript, error) {
		if !reflect.DeepEqual(got.CaptionTracks, metadata.CaptionTracks) {
			t.Fatalf("old metadata used: %+v", got)
		}
		return &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "Original English", Language: "en", CaptionTrack: &track}, nil
	}
	adapter := store.NewFile(dir)
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: adapter, AI: &aiStub{}, Prompts: &promptStub{}})
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := tldw.ParseVideoRef(testVideoID)
	for range 2 {
		got, err := engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly})
		if err != nil || got.Text != "Original English" || got.CaptionTrack == nil || *got.CaptionTrack != track {
			t.Fatalf("got %+v, err %v", got, err)
		}
	}
	if video.metadataCalls != 1 || video.captionCalls != 1 {
		t.Fatalf("metadata %d captions %d", video.metadataCalls, video.captionCalls)
	}
	loaded, err := adapter.LoadMetadata(testVideoID)
	if err != nil || !reflect.DeepEqual(loaded.CaptionTracks, metadata.CaptionTracks) {
		t.Fatalf("provenance not persisted: %+v, %v", loaded, err)
	}
}
