package store_test

import (
	"testing"

	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

func TestFileRoundTripsTranscriptAndMetadata(t *testing.T) {
	adapter := store.NewFile(t.TempDir())
	transcript := &tldw.Transcript{
		VideoID: "dQw4w9WgXcQ",
		Source:  tldw.TranscriptSourceCaptions,
		Segments: []tldw.TranscriptSegment{
			{Start: 1, End: 2, Text: "Hello world"},
		},
	}
	if err := adapter.SaveTranscript(transcript); err != nil {
		t.Fatalf("SaveTranscript() error = %v", err)
	}
	loaded, err := adapter.LoadTranscript(transcript.VideoID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}
	if got := loaded.PlainText(); got != "Hello world" {
		t.Fatalf("LoadTranscript().PlainText() = %q, want Hello world", got)
	}
	plain, err := adapter.LoadPlainTranscript(transcript.VideoID)
	if err != nil {
		t.Fatalf("LoadPlainTranscript() error = %v", err)
	}
	if plain != "Hello world" {
		t.Fatalf("LoadPlainTranscript() = %q, want Hello world", plain)
	}

	metadata := &tldw.VideoMetadata{Title: "Example", Channel: "Channel"}
	if err := adapter.SaveMetadata(transcript.VideoID, metadata); err != nil {
		t.Fatalf("SaveMetadata() error = %v", err)
	}
	loadedMetadata, err := adapter.LoadMetadata(transcript.VideoID)
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	if loadedMetadata.Title != metadata.Title || loadedMetadata.CacheVersion != store.MetadataCacheVersion {
		t.Fatalf("LoadMetadata() = %+v, want title and cache version", loadedMetadata)
	}
}

func TestFileRejectsVideoIDPathTraversal(t *testing.T) {
	adapter := store.NewFile(t.TempDir())
	if _, err := adapter.LoadPlainTranscript("../outside"); err == nil {
		t.Fatal("LoadPlainTranscript() accepted an invalid video ID")
	}
	if err := adapter.SaveTranscript(&tldw.Transcript{VideoID: "../outside", Text: "secret"}); err == nil {
		t.Fatal("SaveTranscript() accepted an invalid video ID")
	}
}
