package tldw_test

import (
	"context"
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
