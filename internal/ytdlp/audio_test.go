package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestAudioUsesConfiguredCacheDir(t *testing.T) {
	for _, tc := range []struct {
		url, mediaID, cacheKey, format string
	}{
		{"dQw4w9WgXcQ", "dQw4w9WgXcQ", "dQw4w9WgXcQ", "bestaudio"},
		{"https://x.com/creatine_cycle/status/2123846497540489345", "2123844682560290816", "x.2123846497540489345", "bestaudio/best"},
	} {
		t.Run(tc.cacheKey, func(t *testing.T) {
			cacheDir := t.TempDir()
			yt := New(t.TempDir(), cacheDir, false, true)
			yt.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if i := slices.Index(args, "-f"); i < 0 || args[i+1] != tc.format {
					t.Fatalf("unexpected audio format in %v", args)
				}
				i := slices.Index(args, "-o")
				if i < 0 {
					t.Fatal("missing output template")
				}
				path := strings.NewReplacer("%(id)s", tc.mediaID, "%(ext)s", "mp3").Replace(args[i+1])
				return nil, os.WriteFile(path, []byte("audio"), 0o600)
			})
			ref, err := tldw.ParseVideoRef(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			got, err := yt.DownloadAudio(t.Context(), ref)
			if err != nil || got != filepath.Join(cacheDir, tc.cacheKey+".mp3") {
				t.Fatalf("DownloadAudio() = %q, %v", got, err)
			}
		})
	}
}

func TestCanceledAudioIsNeverPublished(t *testing.T) {
	cache := t.TempDir()
	yt := New(t.TempDir(), cache, false, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	yt.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		path := strings.ReplaceAll(args[slices.Index(args, "-o")+1], "%(ext)s", "mp3")
		err := os.WriteFile(path, []byte("partial"), 0600)
		cancel()
		return nil, err
	})
	if _, err := yt.DownloadAudio(ctx, ref); err != context.Canceled {
		t.Fatalf("error = %v", err)
	}
	files, err := os.ReadDir(cache)
	if err != nil || len(files) != 0 {
		t.Fatalf("leftover files: %v, %v", files, err)
	}
}
