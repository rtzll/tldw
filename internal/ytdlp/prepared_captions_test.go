package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

type inputRunnerFunc func(context.Context, io.Reader, string, ...string) ([]byte, error)

func (f inputRunnerFunc) RunWithInput(ctx context.Context, input io.Reader, name string, args ...string) ([]byte, error) {
	return f(ctx, input, name, args...)
}

func TestPreparedCaptionRecoveryUsesFreshLookup(t *testing.T) {
	for _, tc := range []struct {
		name, diagnostic string
		wait             time.Duration
	}{
		{"empty response", "ERROR: Did not get any data blocks", time.Second},
		{"missing prepared file", "", time.Second},
		{"rate limit", "HTTP Error 429: Too Many Requests\nRetry-After: 3", 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				video := New(dir, dir, false, true)
				ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
				track := directTrack{Language: "en-orig", Automatic: true}
				const metadata = `{"id":"dQw4w9WgXcQ","_type":"video","title":"Test","channel":"Channel",
					"webpage_url":"https://example.org/watch","original_url":"https://example.org/watch",
					"additional_urls":["https://example.org/extra"],"http_headers":{"User-Agent":"test"},
					"automatic_captions":{"en-orig":[{"url":"https://example.org/sub?signed=private","ext":"vtt","__yt_dlp_client":"web"}]},
					"subtitles":{"fr":[{"url":"https://example.org/other"}]}}`
				var lookups, downloads int
				video.executor = inputRunnerFunc(func(_ context.Context, input io.Reader, _ string, args ...string) ([]byte, error) {
					if slices.Contains(args, "--dump-single-json") {
						lookups++
						return []byte(metadata), nil
					}
					downloads++
					if downloads == 1 {
						if input == nil || !slices.Contains(args, "--load-info-json") || args[len(args)-1] != "-" || slices.Contains(args, ref.URL()) {
							t.Fatalf("missing single-use stdin handoff: %v", args)
						}
						var info map[string]json.RawMessage
						if err := json.NewDecoder(input).Decode(&info); err != nil {
							t.Fatal(err)
						}
						for _, field := range []string{"webpage_url", "original_url", "additional_urls", "subtitles"} {
							if _, ok := info[field]; ok {
								t.Fatalf("unexpected field in caption input: %s", field)
							}
						}
						if !strings.Contains(string(info["automatic_captions"]), "signed=private") ||
							!strings.Contains(string(info["automatic_captions"]), "__yt_dlp_client") || info["http_headers"] == nil {
							t.Fatal("prepared input lost selected track or request details")
						}
						// A concurrent metadata refresh must not change this request's retry.
						video.prepared.replace(ref.ID(), track, []byte("new lookup"))
						if tc.diagnostic == "" {
							return nil, nil
						}
						return nil, errors.New(tc.diagnostic)
					}
					if input != nil || args[len(args)-1] != ref.URL() || slices.Contains(args, "--load-info-json") {
						t.Fatalf("retry did not rediscover the track: %v", args)
					}
					path := args[slices.Index(args, "-o")+1] + ".en-orig.srt"
					return nil, os.WriteFile(path, []byte("1\n00:00:00,000 --> 00:00:01,000\nHello\n"), 0600)
				})
				engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: store.NewFile(dir), AI: unusedDependencies{}, Prompts: unusedDependencies{}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := engine.MetadataFor(t.Context(), ref); err != nil {
					t.Fatal(err)
				}
				disk, err := os.ReadFile(filepath.Join(dir, ref.ID()+".meta.json"))
				if err != nil || strings.Contains(string(disk), "signed=private") {
					t.Fatalf("expected a persistent metadata record without signed URLs, err=%v", err)
				}
				start := time.Now()
				transcript, err := engine.Transcript(t.Context(), ref, tldw.TimestampedCaptions)
				if err != nil || !transcript.HasTimestamps() || transcript.PlainText() != "Hello" ||
					lookups != 1 || downloads != 2 || time.Since(start) != tc.wait {
					t.Fatalf("metadata=%d downloads=%d elapsed=%s err=%v", lookups, downloads, time.Since(start), err)
				}
			})
		})
	}
}

func TestPreparedCaptionsBoundedAndSingleUse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var pending preparedCaptions
		track := directTrack{Language: "en"}
		for i := range maxPreparedVideos + 1 {
			pending.replace(fmt.Sprint(i), track, []byte(fmt.Sprint(i)))
		}
		if len(pending.byVideo) != maxPreparedVideos || pending.take(fmt.Sprint(maxPreparedVideos), track) != nil {
			t.Fatal("exceeded pending video limit")
		}
		if pending.take("0", directTrack{Language: "de"}) != nil || string(pending.take("0", track)) != "0" || pending.take("0", track) != nil {
			t.Fatal("handoff must match the track and be consumed exactly once")
		}
		pending.replace("1", track, make([]byte, maxCaptionInfoBytes+1))
		if pending.take("1", track) != nil {
			t.Fatal("oversized refresh should discard the old entry and use a normal lookup")
		}
		time.Sleep(captionInfoLifetime / 2)
		pending.replace("2", track, []byte("replacement"))
		time.Sleep(captionInfoLifetime / 2)
		synctest.Wait()
		if len(pending.byVideo) != 1 || string(pending.take("2", track)) != "replacement" {
			t.Fatal("expiry should discard only the superseded generation and older entries")
		}
	})
}
