package store_test

import (
	"encoding/json"
	"errors"
	"fmt"
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

	scan, err := store.NewFile(dir).ListMetadata()
	if err != nil {
		t.Fatalf("ListMetadata() error = %v", err)
	}
	if len(scan.Entries) != 1 {
		t.Fatalf("ListMetadata() returned %d entries, want 1", len(scan.Entries))
	}
	entry := scan.Entries[0]
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
	scan, err := adapter.ListMetadata()
	if err != nil {
		t.Fatalf("ListMetadata() error = %v", err)
	}
	if len(scan.Entries) != 1 || !scan.Entries[0].FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("ListMetadata() = %+v, want first seen %v", scan.Entries, firstSeen)
	}
	if scan.Entries[0].Metadata.Title != "Fresh" || scan.Entries[0].Metadata.Duration != 120 {
		t.Fatalf("ListMetadata() metadata = %+v, want refreshed metadata", scan.Entries[0].Metadata)
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

	scan, err := store.NewFile(dir).ListMetadata()
	if err != nil {
		t.Fatalf("ListMetadata() error = %v", err)
	}
	if len(scan.Entries) != 1 || !scan.Entries[0].FirstSeenAt.Equal(modifiedAt) {
		t.Fatalf("ListMetadata() = %+v, want modification time %v", scan.Entries, modifiedAt)
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

	scan, err := store.NewFile(dir).ListMetadata()
	if err != nil {
		t.Fatalf("ListMetadata() error = %v", err)
	}
	if len(scan.Entries) != 1 || scan.Entries[0].VideoID != "dQw4w9WgXcQ" {
		t.Fatalf("ListMetadata() = %+v, want only the video cache", scan.Entries)
	}
}

func TestFileRoundTripsTranscriptAndMetadata(t *testing.T) {
	adapter := store.NewFile(t.TempDir())
	transcript, err := tldw.NewCaptionTranscript("dQw4w9WgXcQ", tldw.CaptionTrack{Language: "en-orig", Automatic: true, Direct: true}, "", []tldw.TranscriptSegment{{Start: 1, End: 2, Text: "Hello world"}})
	if err != nil {
		t.Fatal(err)
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
	if transcript.Source() != "" {
		t.Fatalf("LoadTranscript().Source = %q, want unknown source", transcript.Source())
	}
}

func TestFileRefreshesOldCaptionsOnly(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("version %d", version), func(t *testing.T) {
			for _, source := range []tldw.TranscriptSource{tldw.TranscriptSourceCaptions, tldw.TranscriptSourcePaidTranscription, ""} {
				t.Run(string(source), func(t *testing.T) {
					dir := t.TempDir()
					const id, content = "dQw4w9WgXcQ", "old transcript"
					data, err := json.Marshal(map[string]any{"cache_version": version, "video_id": id, "source": source, "text": content})
					if err != nil {
						t.Fatal(err)
					}
					for suffix, contents := range map[string][]byte{".transcript.json": data, ".txt": []byte(content)} {
						if err := os.WriteFile(filepath.Join(dir, id+suffix), contents, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					got, err := store.NewFile(dir).LoadTranscript(id)
					if source == tldw.TranscriptSourceCaptions {
						// Do not fall back to the equally stale plain text file.
						if !errors.Is(err, tldw.ErrStoreStale) {
							t.Fatalf("old captions: got %+v, %v; want ErrStoreStale", got, err)
						}
					} else if err != nil || got.PlainText() != content || got.Source() != source {
						t.Fatalf("non-caption cache: got %+v, %v", got, err)
					}
				})
			}
		})
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
	input, err := tldw.NewLegacyTranscript("../outside", "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SaveTranscript(input); err == nil {
		t.Fatal("SaveTranscript() accepted an invalid video ID")
	}
}

func TestMetadataLoadPreservesFreshness(t *testing.T) {
	dir := t.TempDir()
	for _, field := range []string{"updated_at", "cached_at"} {
		data := fmt.Sprintf(`{"cache_version":4,"channel":"Channel","%s":"2026-09-06T12:00:00Z"}`, field)
		if err := os.WriteFile(filepath.Join(dir, "dQw4w9WgXcQ.meta.json"), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		metadata, err := store.NewFile(dir).LoadMetadata("dQw4w9WgXcQ")
		if err != nil {
			t.Fatal(err)
		}
		if !metadata.CheckedAt.Equal(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)) {
			t.Fatalf("%s lost freshness: %+v", field, metadata)
		}
	}
}

func TestListMetadataReturnsHealthyEntriesAndWarning(t *testing.T) {
	dir := t.TempDir()
	adapter := store.NewFile(dir)
	if err := adapter.SaveMetadata("dQw4w9WgXcQ", &tldw.VideoMetadata{Title: "Good", Duration: 12}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tAP1eZYEuKA.meta.json"), []byte("invalid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	scan, err := adapter.ListMetadata()
	if err != nil || len(scan.Warnings) != 1 {
		t.Fatalf("warning = %v", err)
	}
	if len(scan.Entries) != 1 || scan.Entries[0].Metadata.Title != "Good" {
		t.Fatalf("entries = %+v", scan.Entries)
	}
}

func TestMetadataScanDistinguishesMissingDirectoryFromFailedListing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library")
	store := store.NewFile(path)
	scan, err := store.ListMetadata()
	if err != nil || len(scan.Entries) != 0 || len(scan.Warnings) != 0 {
		t.Fatalf("missing directory: %+v, %v", scan, err)
	}
	if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListMetadata(); err == nil {
		t.Fatal("unreadable library reported as empty")
	}
}

func TestLegacyPaidCacheAndFailedReplacement(t *testing.T) {
	dir := t.TempDir()
	const key = "dQw4w9WgXcQ"
	if err := os.WriteFile(filepath.Join(dir, key+".transcript.json"), []byte(`{"source":"whisper","text":"already paid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := store.NewFile(dir)
	if err := s.SaveTranscript(&tldw.Transcript{VideoID: key}); err == nil {
		t.Fatal("accepted empty replacement")
	}
	got, err := s.LoadTranscript(key)
	if err != nil || got.Source() != tldw.TranscriptSourcePaidTranscription || got.PlainText() != "already paid" || got.HasTimestamps() {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCanonicalTranscriptSurvivesFailedCompatibilityWrite(t *testing.T) {
	dir := t.TempDir()
	const key = "dQw4w9WgXcQ"
	if err := os.Mkdir(filepath.Join(dir, key+".txt"), 0700); err != nil {
		t.Fatal(err)
	}
	s := store.NewFile(dir)
	paid, err := tldw.NewPaidTranscript(key, "saved")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTranscript(paid); err == nil {
		t.Fatal("expected compatibility write error")
	}
	got, err := s.LoadTranscript(key)
	if err != nil || got.PlainText() != "saved" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
