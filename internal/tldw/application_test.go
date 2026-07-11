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
	fixture := newEngineFixture(t, tldw.Config{WhisperTimeout: time.Minute})

	if _, err := fixture.engine.Transcript(context.Background(), videoRef(t), tldw.TranscriptRequest{
		Policy: tldw.TranscriptPolicyWhisperOnly,
	}); err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if !fixture.ai.sawDeadline {
		t.Fatal("AI Transcribe context had no deadline")
	}
}

func TestEngineVideoOnlyMethodsRejectPlaylist(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	playlist := playlistRef(t)

	if _, err := fixture.engine.Transcript(context.Background(), playlist, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyCaptionsOnly}); err == nil {
		t.Fatal("Transcript() accepted a playlist")
	}
	if _, err := fixture.engine.MetadataFor(context.Background(), playlist); err == nil {
		t.Fatal("MetadataFor() accepted a playlist")
	}
}

func TestEngineRefreshesIncompleteCachedMetadata(t *testing.T) {
	fixture := newEngineFixture(t, tldw.Config{})
	fixture.store.metadata = &tldw.VideoMetadata{Title: "Cached", HasCaptions: true, CaptionLanguages: []string{"en"}}
	fixture.video.metadata = &tldw.VideoMetadata{Title: "Fresh", Channel: "AI Engineer", Creators: []string{"AI Engineer", "Matt Pocock"}}

	metadata, err := fixture.engine.MetadataFor(context.Background(), videoRef(t))
	if err != nil {
		t.Fatalf("MetadataFor() error = %v", err)
	}
	if metadata.Title != "Fresh" || metadata.Channel != "AI Engineer" || len(metadata.Creators) != 2 {
		t.Fatalf("MetadataFor() = %+v", metadata)
	}
	if fixture.video.metadataCalls != 1 || fixture.store.metadataSaves != 1 || fixture.store.metadata != metadata {
		t.Fatalf("metadata calls = %d, saves = %d, cached = %+v", fixture.video.metadataCalls, fixture.store.metadataSaves, fixture.store.metadata)
	}
}
