package store_test

import (
	"errors"
	"os"
	"path/filepath"
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
	metadata := &tldw.VideoMetadata{Title: "Example", Channel: "Channel"}
	if err := adapter.SaveMetadata(transcript.VideoID, metadata); err != nil {
		t.Fatalf("SaveMetadata() error = %v", err)
	}
	loadedMetadata, err := adapter.LoadMetadata(transcript.VideoID)
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	if loadedMetadata.Title != metadata.Title {
		t.Fatalf("LoadMetadata() = %+v, want title", loadedMetadata)
	}
}

func TestFileLoadsLegacyPlainTranscriptWithoutInventingItsSource(t *testing.T) {
	dir := t.TempDir()
	if err := store.SavePlainTranscript("dQw4w9WgXcQ", "legacy transcript", dir); err != nil {
		t.Fatalf("SavePlainTranscript() error = %v", err)
	}

	transcript, err := store.NewFile(dir).LoadTranscript("dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}
	if transcript.PlainText() != "legacy transcript" {
		t.Fatalf("LoadTranscript().PlainText() = %q, want legacy transcript", transcript.PlainText())
	}
	if transcript.Source != "" {
		t.Fatalf("LoadTranscript().Source = %q, want unknown source", transcript.Source)
	}
}

func TestFileTreatsOldMetadataSchemaAsCacheMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dQw4w9WgXcQ.meta.json")
	if err := os.WriteFile(path, []byte(`{"cache_version":2,"title":"Old"}`), 0o644); err != nil {
		t.Fatalf("write stale metadata: %v", err)
	}
	_, err := store.NewFile(dir).LoadMetadata("dQw4w9WgXcQ")
	if !errors.Is(err, tldw.ErrStoreStale) {
		t.Fatalf("LoadMetadata() error = %v, want ErrStoreStale", err)
	}
}

func TestFileIdentifiesMissingEntries(t *testing.T) {
	adapter := store.NewFile(t.TempDir())

	if _, err := adapter.LoadTranscript("dQw4w9WgXcQ"); !errors.Is(err, tldw.ErrStoreNotFound) {
		t.Fatalf("LoadTranscript() error = %v, want ErrStoreNotFound", err)
	}
	if _, err := adapter.LoadMetadata("dQw4w9WgXcQ"); !errors.Is(err, tldw.ErrStoreNotFound) {
		t.Fatalf("LoadMetadata() error = %v, want ErrStoreNotFound", err)
	}
}

func TestFileRejectsVideoIDPathTraversal(t *testing.T) {
	adapter := store.NewFile(t.TempDir())
	if _, err := adapter.LoadTranscript("../outside"); err == nil {
		t.Fatal("LoadTranscript() accepted an invalid video ID")
	}
	if err := adapter.SaveTranscript(&tldw.Transcript{VideoID: "../outside", Text: "secret"}); err == nil {
		t.Fatal("SaveTranscript() accepted an invalid video ID")
	}
}
