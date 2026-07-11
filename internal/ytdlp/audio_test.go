package ytdlp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestAudioUsesConfiguredCacheDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	yt := NewYouTubeWithCache(t.TempDir(), cacheDir, false, true)
	yt.executor = &mockCommandRunner{}
	ref, err := tldw.ParseVideoRef("dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	got, err := yt.DownloadAudio(context.Background(), ref)
	if err != nil {
		t.Fatalf("DownloadAudio() error = %v", err)
	}

	want := filepath.Join(cacheDir, "dQw4w9WgXcQ.mp3")
	if got != want {
		t.Fatalf("DownloadAudio() = %q, want %q", got, want)
	}
}
