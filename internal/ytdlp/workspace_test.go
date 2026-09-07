package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestConcurrentDownloadsUsePrivateWorkspaces(t *testing.T) {
	for _, kind := range []string{"audio", "captions"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			data := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				started := make(chan string, 2)
				release := make(chan struct{})
				yt := NewYouTube(data, root, false, true)
				yt.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
					for i, arg := range args {
						if arg != "-o" {
							continue
						}
						path := strings.ReplaceAll(args[i+1], "%(id)s", "dQw4w9WgXcQ")
						text := "audio"
						if kind == "audio" {
							path = strings.ReplaceAll(path, "%(ext)s", "mp3")
						} else {
							path += ".en.srt"
							text = "1\n00:00:00,000 --> 00:00:01,000\nHello\n"
						}
						started <- filepath.Dir(path)
						<-release
						return nil, os.WriteFile(path, []byte(text), 0644)
					}
					return nil, fmt.Errorf("missing output path")
				})
				ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
				results := make(chan error, 2)
				for range 2 {
					go func() {
						var err error
						if kind == "audio" {
							_, err = yt.DownloadAudio(context.Background(), ref)
						} else {
							_, err = yt.FetchCaptions(context.Background(), ref, []string{"en"}, "en")
						}
						results <- err
					}()
				}
				first, second := <-started, <-started
				if first == second {
					t.Error("shared download directory")
				}
				close(release)
				for range 2 {
					if err := <-results; err != nil {
						t.Error(err)
					}
				}
				for _, dir := range []string{first, second} {
					if _, err := os.Stat(dir); !os.IsNotExist(err) {
						t.Errorf("workspace not cleaned: %s: %v", dir, err)
					}
				}
			})
		})
	}
}
