package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestAudioUsesConfiguredCacheDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	yt := NewYouTube(t.TempDir(), cacheDir, false, true)
	yt.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for i, arg := range args {
			if arg == "-o" {
				path := strings.ReplaceAll(strings.ReplaceAll(args[i+1], "%(id)s", "dQw4w9WgXcQ"), "%(ext)s", "mp3")
				return nil, os.WriteFile(path, []byte("audio"), 0644)
			}
		}
		return nil, nil
	})
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
