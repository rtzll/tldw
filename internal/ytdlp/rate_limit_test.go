package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rtzll/tldw/internal/process"
	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

func TestYouTubeCooldownAcrossProvidersAndCache(t *testing.T) {
	dir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		client := New(dir, dir, false, true)
		youtube, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
		x, _ := tldw.ParseVideoRef("https://x.com/i/web/status/123")
		metadata := &tldw.VideoMetadata{CaptionTracks: []tldw.CaptionTrack{{Language: "en", Direct: true}}}
		now := time.Now()
		calls := 0
		client.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls++
			if args[len(args)-1] == x.URL() {
				if slices.Contains(args, "--dump-single-json") {
					return []byte(`{"display_id":"123","formats":[{}]}`), nil
				}
				return nil, errors.New("HTTP Error 429: Too Many Requests\nRetry-After: 600")
			}
			if calls == 1 {
				return nil, errors.New("HTTP Error 429\nRetry-After: 300")
			}
			return []byte("{}"), nil
		})
		_, err := client.FetchMetadata(t.Context(), youtube)
		var limited *tldw.YouTubeRateLimitError
		if !errors.As(err, &limited) || limited.RetryAfter != 5*time.Minute {
			t.Fatalf("metadata error = %v", err)
		}
		if _, err := client.FetchMetadata(t.Context(), x); err != nil {
			t.Fatalf("X metadata during YouTube cooldown: %v", err)
		}
		_, err = client.FetchCaptions(t.Context(), x, metadata)
		if !errors.Is(err, tldw.ErrRateLimited) || errors.As(err, &limited) || calls != 3 {
			t.Fatalf("X captions: calls=%d err=%v", calls, err)
		}
		time.Sleep(10 * time.Second)
		_, err = client.FetchCaptions(t.Context(), youtube, metadata)
		if !errors.As(err, &limited) || limited.RetryAfter != 290*time.Second || calls != 3 {
			t.Fatalf("YouTube captions: calls=%d err=%v", calls, err)
		}
		path := filepath.Join(dir, youtube.ID()+".en-orig.srt")
		if err := os.WriteFile(path, []byte("1\n00:00:00,000 --> 00:00:01,000\nCached\n"), 0600); err != nil {
			t.Fatal(err)
		}
		original := &tldw.VideoMetadata{CaptionTracks: []tldw.CaptionTrack{{Language: "en-orig", Direct: true, Automatic: true}}}
		transcript, err := client.FetchCaptions(t.Context(), youtube, original)
		if err != nil || transcript.PlainText() != "Cached" || calls != 3 || time.Since(now) != 10*time.Second {
			t.Fatalf("cached captions: calls=%d err=%v", calls, err)
		}
		time.Sleep(290 * time.Second)
		if _, err := client.FetchMetadata(t.Context(), youtube); err != nil || calls != 4 {
			t.Fatalf("YouTube recovery: calls=%d err=%v", calls, err)
		}
	})
}

// Only the free video read path is expected to run in this test.
type unusedDependencies struct {
	tldw.AIAdapter
	tldw.PromptBuilder
}

func TestTranscriptSharesOneRateLimitRetryAcrossMetadataAndCaptions(t *testing.T) {
	dir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		client := New(dir, dir, false, true)
		var messages strings.Builder
		client.SetLogSink(log.New(&messages, "", 0))
		var metadataCalls, captionCalls int
		client.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if slices.Contains(args, "--dump-single-json") {
				metadataCalls++
				if metadataCalls == 2 {
					return []byte(`{"channel":"Channel","subtitles":{"en":[{"url":"https://example.com/sub"}]}}`), nil
				}
			} else if slices.Contains(args, "--write-subs") {
				captionCalls++
			} else {
				t.Fatalf("unexpected yt-dlp call: %v", args)
			}
			return nil, &process.CommandError{Stderr: "HTTP Error 429\nRetry-After: 3", Err: errors.New("exit 1")}
		})
		engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: client, Store: store.NewFile(dir), AI: unusedDependencies{}, Prompts: unusedDependencies{}})
		if err != nil {
			t.Fatal(err)
		}
		ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
		start := time.Now()
		_, err = engine.Transcript(t.Context(), ref, tldw.CaptionsThenPaid)
		if !errors.Is(err, tldw.ErrRateLimited) || !strings.Contains(err.Error(), "3 seconds") ||
			metadataCalls != 2 || captionCalls != 1 || time.Since(start) != 3*time.Second {
			t.Fatalf("metadata=%d captions=%d elapsed=%s error=%v", metadataCalls, captionCalls, time.Since(start), err)
		}
		if !strings.Contains(messages.String(), "recovered=true added_time=3s") {
			t.Fatalf("missing retry outcome: %q", messages.String())
		}
	})
}

func TestRetryAfterDiagnostics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		date := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
		for _, tc := range []struct {
			details string
			want    time.Duration
		}{
			{"HTTP Error 429", 0},
			{"Retry-After: invalid", 0},
			{"retry-after: 17\r\n", 17 * time.Second},
			{fmt.Sprintf("yt-dlp: Retry-After: %s\n", date), time.Hour},
		} {
			if got := retryAfter(tc.details); got != tc.want {
				t.Errorf("retryAfter(%q) = %v, want %v", tc.details, got, tc.want)
			}
		}
	})
}
