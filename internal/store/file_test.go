package store_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

func TestFileListsMetadataWithItsOriginalCacheTime(t *testing.T) {
	dir := t.TempDir()
	firstSeen := time.Date(2026, time.July, 3, 14, 30, 0, 0, time.UTC)
	path := filepath.Join(dir, "dQw4w9WgXcQ.meta.json")
	data, err := json.Marshal(map[string]any{
		"cache_version": 3,
		"title":         "Example",
		"duration":      3723.0,
		"cached_at":     firstSeen,
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	entries, err := store.NewFile(dir).ListMetadata()
	if err != nil {
		t.Fatalf("ListMetadata() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ListMetadata() returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.VideoID != "dQw4w9WgXcQ" || entry.Metadata.Title != "Example" || entry.Metadata.Duration != 3723 {
		t.Fatalf("ListMetadata() entry = %+v", entry)
	}
	if !entry.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("ListMetadata() FirstSeenAt = %v, want %v", entry.FirstSeenAt, firstSeen)
	}
}

func TestFilePreservesFirstSeenTimeWhenMetadataIsRefreshed(t *testing.T) {
	dir := t.TempDir()
	firstSeen := time.Date(2025, time.December, 10, 9, 15, 0, 0, time.UTC)
	path := filepath.Join(dir, "dQw4w9WgXcQ.meta.json")
	data := []byte(`{"cache_version":3,"title":"Old","duration":60,"cached_at":"2025-12-10T09:15:00Z"}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	adapter := store.NewFile(dir)
	if err := adapter.SaveMetadata("dQw4w9WgXcQ", &tldw.VideoMetadata{Title: "Fresh", Duration: 120}); err != nil {
		t.Fatalf("SaveMetadata() error = %v", err)
	}
	entries, err := adapter.ListMetadata()
	if err != nil {
		t.Fatalf("ListMetadata() error = %v", err)
	}
	if len(entries) != 1 || !entries[0].FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("ListMetadata() = %+v, want first seen %v", entries, firstSeen)
	}
	if entries[0].Metadata.Title != "Fresh" || entries[0].Metadata.Duration != 120 {
		t.Fatalf("ListMetadata() metadata = %+v, want refreshed metadata", entries[0].Metadata)
	}
}

func TestFileUsesModificationTimeForMetadataWithoutCacheTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dQw4w9WgXcQ.meta.json")
	if err := os.WriteFile(path, []byte(`{"cache_version":1,"title":"Legacy","duration":300}`), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	modifiedAt := time.Date(2024, time.March, 2, 8, 45, 0, 0, time.UTC)
	if err := os.Chtimes(path, modifiedAt, modifiedAt); err != nil {
		t.Fatalf("set metadata modification time: %v", err)
	}

	entries, err := store.NewFile(dir).ListMetadata()
	if err != nil {
		t.Fatalf("ListMetadata() error = %v", err)
	}
	if len(entries) != 1 || !entries[0].FirstSeenAt.Equal(modifiedAt) {
		t.Fatalf("ListMetadata() = %+v, want modification time %v", entries, modifiedAt)
	}
}

func TestFileListMetadataIgnoresNonVideoCacheFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"dQw4w9WgXcQ.meta.json":                        `{"cache_version":3,"title":"Video","duration":300,"cached_at":"2026-07-01T10:00:00Z"}`,
		"PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq.meta.json": `{"cache_version":3,"title":"Playlist","cached_at":"2026-06-01T10:00:00Z"}`,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	entries, err := store.NewFile(dir).ListMetadata()
	if err != nil {
		t.Fatalf("ListMetadata() error = %v", err)
	}
	if len(entries) != 1 || entries[0].VideoID != "dQw4w9WgXcQ" {
		t.Fatalf("ListMetadata() = %+v, want only the video cache", entries)
	}
}

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
	path := filepath.Join(dir, "dQw4w9WgXcQ.txt")
	if err := os.WriteFile(path, []byte("legacy transcript"), 0o644); err != nil {
		t.Fatalf("writing legacy transcript: %v", err)
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
