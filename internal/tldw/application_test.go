package tldw_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestNewEngineRejectsInvalidDependenciesAndConfig(t *testing.T) {
	valid := tldw.Dependencies{
		Video: &videoStub{}, Store: &memoryStore{}, AI: &aiStub{}, Prompts: &promptStub{},
	}
	tests := map[string]func(*tldw.Dependencies){
		"video":   func(dependencies *tldw.Dependencies) { dependencies.Video = nil },
		"store":   func(dependencies *tldw.Dependencies) { dependencies.Store = nil },
		"AI":      func(dependencies *tldw.Dependencies) { dependencies.AI = nil },
		"prompts": func(dependencies *tldw.Dependencies) { dependencies.Prompts = nil },
	}
	for name, remove := range tests {
		t.Run(name, func(t *testing.T) {
			dependencies := valid
			remove(&dependencies)
			if _, err := tldw.NewEngine(tldw.Config{}, dependencies); err == nil {
				t.Fatalf("NewEngine() accepted missing %s dependency", name)
			}
		})
	}
	if _, err := tldw.NewEngine(tldw.Config{WhisperTimeout: -time.Second}, valid); err == nil {
		t.Fatal("NewEngine() accepted a negative Whisper timeout")
	}
}

func TestEngineAppliesWhisperTimeout(t *testing.T) {
	video := &videoStub{audioPath: "audio.mp3"}
	ai := &aiStub{transcription: "transcript"}
	engine, err := tldw.NewEngine(tldw.Config{WhisperTimeout: time.Minute}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: ai, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	if _, err := engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyWhisperOnly,
	}); err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if !ai.sawDeadline {
		t.Fatal("AI Transcribe context had no deadline")
	}
}

func TestEngineVideoOnlyMethodsRejectPlaylist(t *testing.T) {
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: &videoStub{}, Store: &memoryStore{}, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	playlist, err := tldw.ParseReference("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}

	if _, err := engine.Transcript(context.Background(), playlist, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly}); err == nil {
		t.Fatal("Transcript() accepted a playlist")
	}
	if _, err := engine.MetadataFor(context.Background(), playlist); err == nil {
		t.Fatal("MetadataFor() accepted a playlist")
	}
}

func TestEngineRefreshesIncompleteCachedMetadata(t *testing.T) {
	store := &memoryStore{metadata: &tldw.VideoMetadata{Title: "Cached", HasCaptions: true, CaptionLanguages: []string{"en"}}}
	video := &videoStub{metadata: &tldw.VideoMetadata{Title: "Fresh", Channel: "AI Engineer", Creators: []string{"AI Engineer", "Matt Pocock"}}}
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

	metadata, err := engine.MetadataFor(context.Background(), ref)
	if err != nil {
		t.Fatalf("MetadataFor() error = %v", err)
	}
	if metadata.Title != "Fresh" || metadata.Channel != "AI Engineer" || len(metadata.Creators) != 2 {
		t.Fatalf("MetadataFor() = %+v", metadata)
	}
	if video.metadataCalls != 1 || store.metadataSaves != 1 || store.metadata != metadata {
		t.Fatalf("metadata calls = %d, saves = %d, cached = %+v", video.metadataCalls, store.metadataSaves, store.metadata)
	}
}

func TestEngineRejectsEmptyAdapterResults(t *testing.T) {
	video := &videoStub{}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: video, Store: &memoryStore{}, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	videoRef, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}
	playlistRef, err := tldw.ParseReference("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}

	if _, err := engine.MetadataFor(context.Background(), videoRef); err == nil {
		t.Fatal("MetadataFor() accepted nil metadata from the video adapter")
	}
	if _, err := engine.CreatePlaylistSummary(context.Background(), playlistRef, tldw.PlaylistSummaryRequest{
		Transcript: tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly},
	}); err == nil {
		t.Fatal("CreatePlaylistSummary() accepted nil playlist data from the video adapter")
	}
}

func TestNegativeCaptionCacheRefresh(t *testing.T) {
	for _, tt := range []struct {
		name        string
		age         time.Duration
		paid        bool
		fail        bool
		wantRefresh bool
	}{
		{"expired", time.Hour, false, false, true},
		{"recent", time.Minute, false, false, false},
		{"recheck before paid fallback", time.Minute, true, false, true},
		{"failed recheck does not spend", time.Minute, true, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lookupErr := errors.New("upstream unavailable")
			video := &videoStub{metadata: &tldw.VideoMetadata{Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en"}}, captions: &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "captions became available"}}
			if tt.fail {
				video.metadataErr = lookupErr
			}
			cache := &memoryStore{metadata: &tldw.VideoMetadata{Channel: "Channel", CheckedAt: time.Now().Add(-tt.age)}}
			ai := &aiStub{}
			engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: cache, AI: ai, Prompts: &promptStub{}})
			if err != nil {
				t.Fatal(err)
			}
			ref, _ := tldw.ParseVideoRef(testVideoID)
			policy := tldw.TranscriptPolicyCaptionsOnly
			if tt.paid {
				policy = tldw.TranscriptPolicyCaptionsThenWhisper
			}
			result, err := engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: policy})
			if tt.fail {
				if !errors.Is(err, lookupErr) {
					t.Fatalf("error=%v", err)
				}
			} else if tt.wantRefresh {
				if err != nil || result.Source != tldw.TranscriptSourceCaptions {
					t.Fatalf("result=%+v error=%v", result, err)
				}
			} else if !errors.Is(err, tldw.ErrCaptionsUnavailable) {
				t.Fatalf("error=%v", err)
			}
			if (video.metadataCalls > 0) != tt.wantRefresh {
				t.Fatalf("metadata calls=%d", video.metadataCalls)
			}
			if ai.transcribeCalls != 0 || video.audioCalls != 0 {
				t.Fatal("negative cache caused paid work")
			}
			if tt.wantRefresh && !tt.fail {
				if _, err := engine.MetadataFor(context.Background(), ref); err != nil {
					t.Fatal(err)
				}
				if video.metadataCalls != 1 {
					t.Fatal("fresh metadata was fetched again")
				}
			}
		})
	}
}

func TestExplicitMetadataRefreshBypassesMemory(t *testing.T) {
	video := &videoStub{metadata: &tldw.VideoMetadata{Title: "Fresh", Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en"}}}
	cache := &memoryStore{metadata: &tldw.VideoMetadata{Title: "Cached", Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en"}}}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: cache, AI: &aiStub{}, Prompts: &promptStub{}})
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := tldw.ParseVideoRef(testVideoID)
	if _, err := engine.MetadataFor(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	fresh, err := engine.RefreshMetadata(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Title != "Fresh" || fresh.CheckedAt.IsZero() || video.metadataCalls != 1 {
		t.Fatalf("fresh=%+v calls=%d", fresh, video.metadataCalls)
	}
}
