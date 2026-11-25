package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rtzll/tldw/internal/store"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/rtzll/tldw/internal/telemetry"
	"github.com/rtzll/tldw/internal/tldw"
)

func TestEmptyCaptionResponseRetry(t *testing.T) {
	const youtubeID = "v9gt7u_7nWQ"
	for _, tc := range []struct {
		name           string
		input          string
		emptyDownloads int
		wantStarts     []time.Duration
		wantClients    []string
	}{
		{"first succeeds", youtubeID, 0, []time.Duration{0}, []string{"tv_samsung"}},
		{"same player recovers", youtubeID, 1, []time.Duration{0, time.Second}, []string{"web", "web"}},
		{"third recovers", youtubeID, 2, []time.Duration{0, time.Second, 11 * time.Second}, []string{"unknown", "android", "tv_samsung"}},
		{"three empty responses", youtubeID, 3, []time.Duration{0, time.Second, 11 * time.Second}, []string{"web", "web", "web"}},
		{"X stops after two", "https://x.com/example/status/123", 2, []time.Duration{0, time.Second}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				recorder := tracetest.NewSpanRecorder()
				provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
				ctx, root := provider.Tracer("test").Start(t.Context(), "request")
				defer root.End()
				ref, err := tldw.ParseVideoRef(tc.input)
				if err != nil {
					t.Fatal(err)
				}
				disk := store.NewFile(dir)
				err = disk.SaveMetadata(ref.CacheKey(), &tldw.VideoMetadata{
					Channel: "fixture", HasCaptions: true, CaptionLanguages: []string{"en-orig"},
					CaptionTracks: []tldw.CaptionTrack{{Language: "en-orig", Automatic: true, Direct: true}},
				})
				if err != nil {
					t.Fatal(err)
				}
				cause := errors.New("WARNING: private diagnostic\nERROR: Did not get any data blocks\nWARNING: Skipping embedding en-orig subtitle because the file is missing")
				video := New(dir, dir, false, true)
				start := time.Now()
				var starts []time.Duration
				video.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
					starts = append(starts, time.Since(start))
					if !slices.Contains(args, "--write-auto-subs") {
						t.Fatalf("unexpected download: %v", args)
					}
					printAt := slices.Index(args, "--print")
					var output []byte
					if ref.IsX() {
						if printAt >= 0 {
							t.Fatal("YouTube player lookup requested for X")
						}
					} else if printAt < 0 || args[printAt+1] != "video:tldw.caption.player_client=%(requested_subtitles.:.__yt_dlp_client)j" || !slices.Contains(args, "--no-simulate") {
						t.Fatalf("missing selected client or download disabled: %v", args)
					} else if len(starts) > len(tc.wantClients) {
						t.Fatalf("unexpected extra download %d", len(starts))
					} else if client := tc.wantClients[len(starts)-1]; client != "unknown" {
						output = fmt.Appendf(nil, "tldw.caption.player_client=[%q]\n", client)
					}
					if len(starts) <= tc.emptyDownloads {
						return output, cause
					}
					path := args[slices.Index(args, "-o")+1] + ".en-orig.srt"
					return output, os.WriteFile(path, []byte("1\n00:00:00,000 --> 00:00:01,000\nRecovered caption\n"), 0600)
				})
				engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: disk, AI: unusedDependencies{}, Prompts: unusedDependencies{}})
				if err != nil {
					t.Fatal(err)
				}
				got, err := engine.Transcript(ctx, ref, tldw.CaptionsThenPaid)
				if !slices.Equal(starts, tc.wantStarts) {
					t.Fatalf("downloads started at %v, want %v", starts, tc.wantStarts)
				}
				if tc.emptyDownloads < len(tc.wantStarts) {
					if err != nil || got == nil || got.PlainText() != "Recovered caption" {
						t.Fatalf("recovered transcript=%+v err=%v", got, err)
					}
				} else if !errors.Is(err, cause) || !errors.Is(err, tldw.ErrDownloadFailed) || tldw.CanOfferPaidTranscription(err) ||
					!strings.Contains(err.Error(), fmt.Sprintf("after %d attempts", len(starts))) || !strings.Contains(err.Error(), "Try again later") || strings.Contains(err.Error(), "private diagnostic") {
					t.Fatalf("unexpected terminal error: %v", err)
				}
				var emptySpans int
				var clients []string
				for _, span := range recorder.Ended() {
					if span.Name() == "yt-dlp.captions" {
						for _, kv := range span.Attributes() {
							if kv.Key == "error.type" && kv.Value.AsString() == "empty_caption_response" {
								emptySpans++
							}
							if kv.Key == "tldw.caption.player_client" {
								clients = append(clients, kv.Value.AsString())
							}
						}
					}
				}
				if emptySpans != tc.emptyDownloads {
					t.Fatalf("empty download spans=%d, want %d", emptySpans, tc.emptyDownloads)
				}
				if !slices.Equal(clients, tc.wantClients) {
					t.Fatalf("clients=%v, want %v", clients, tc.wantClients)
				}
			})
		})
	}
}

func TestCaptionPlayerClientRecognizesDownloaderDefaults(t *testing.T) {
	for _, client := range []string{"tv_samsung", "visionos", "web", "web_embedded", "web_creator", "web_safari"} {
		output := fmt.Appendf(nil, "other output\n%s[%q]\r\n", captionClientMarker, client)
		if got := captionPlayerClient(output); got != client {
			t.Errorf("player=%q, want %q", got, client)
		}
	}
	for _, value := range []string{"NA", "null", `["web","android"]`, `["private value"]`} {
		if got := captionPlayerClient([]byte(captionClientMarker + value)); got != "unknown" {
			t.Errorf("unexpected exported client %q", got)
		}
	}
}

func TestDownloadTraceKeepsStatusAndExcludesCommandPayload(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var local bytes.Buffer
	p, err := telemetry.NewProvider(t.Context(), "test", &local)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = telemetry.Shutdown(p) }()
	ctx, parent := p.Tracer("test").Start(t.Context(), "operation")
	defer parent.End()
	dir := t.TempDir()
	client := New(dir, dir, false, true)
	client.executor = commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("private transcript body\ntldw.caption.player_client=[\"do-not-export\"]\n"), errors.New("WARNING: HTTP Error 503: retrying\nERROR: Unable to download video subtitles: HTTP Error 403: Forbidden https://example.invalid/?token=do-not-export")
	})
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	_, err = client.downloadCaptions(ctx, ref, directTrack{Language: "en-orig", Automatic: true}, nil)
	if !errors.Is(err, tldw.ErrDownloadFailed) {
		t.Fatal(err)
	}
	var record struct {
		Attrs   map[string]any `json:"attributes"`
		Outcome string         `json:"outcome"`
	}
	decoder := json.NewDecoder(bytes.NewReader(local.Bytes()))
	for decoder.More() {
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
	}
	if record.Outcome != "error" || record.Attrs["http.response.status_code"] != 403.0 || record.Attrs["error.type"] != "subtitle_download_failed" || record.Attrs["tldw.caption.language"] != "en-orig" {
		t.Fatalf("%s", local.String())
	}
	if record.Attrs["tldw.caption.player_client"] != "unknown" {
		t.Fatalf("unexpected player client: %v", record.Attrs["tldw.caption.player_client"])
	}
	for _, secret := range []string{"private transcript", "example.invalid", "do-not-export", dir} {
		if strings.Contains(local.String(), secret) {
			t.Fatal("trace recorded private input")
		}
	}
}

func TestConcurrentVideoDeferredByAnotherTrace(t *testing.T) {
	dir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		tracer := provider.Tracer("test")
		a, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
		b, _ := tldw.ParseVideoRef("tAP1eZYEuKA")
		disk := store.NewFile(dir)
		for _, ref := range []tldw.ContentRef{a, b} {
			err := disk.SaveMetadata(ref.CacheKey(), &tldw.VideoMetadata{
				Channel: "fixture", HasCaptions: true, CaptionLanguages: []string{"en"}, CaptionTracks: []tldw.CaptionTrack{{Language: "en", Direct: true}},
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		video := New(dir, dir, false, true)
		started := make(chan struct{})
		var calls atomic.Int32
		video.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			if args[len(args)-1] != a.URL() {
				t.Errorf("unexpected download for %s", args[len(args)-1])
			}
			return nil, errors.New("HTTP Error 429: Too Many Requests\nRetry-After: 5")
		})
		engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Store: disk, Video: video, AI: unusedDependencies{}, Prompts: unusedDependencies{}})
		if err != nil {
			t.Fatal(err)
		}
		aCtx, rootA := tracer.Start(t.Context(), "request A")
		done := make(chan error, 1)
		go func() {
			_, err := engine.Transcript(aCtx, a, tldw.CaptionsOnly)
			rootA.End()
			done <- err
		}()
		<-started
		synctest.Wait() // A has registered the 429 and is waiting to probe.
		limit, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		bCtx, rootB := tracer.Start(limit, "request B")
		_, err = engine.Transcript(bCtx, b, tldw.CaptionsOnly)
		rootB.End()
		if !errors.Is(err, tldw.ErrRateLimited) || calls.Load() != 1 {
			t.Fatalf("B: err=%v, total downloads=%d", err, calls.Load())
		}
		if err := <-done; !errors.Is(err, tldw.ErrRateLimited) || calls.Load() != 2 {
			t.Fatalf("A: err=%v, total downloads=%d", err, calls.Load())
		}
		var cause, linked trace.SpanID
		var deferred bool
		for _, span := range recorder.Ended() {
			switch span.SpanContext().TraceID() {
			case rootA.SpanContext().TraceID():
				if span.Name() == "youtube.fetch" && !cause.IsValid() {
					cause = span.SpanContext().SpanID()
				}
			case rootB.SpanContext().TraceID():
				if span.Name() == "yt-dlp.captions" || span.Name() == "youtube.fetch" {
					t.Fatal("B trace reports a download")
				}
				for _, link := range span.Links() {
					if link.SpanContext.TraceID() == rootA.SpanContext().TraceID() {
						linked = link.SpanContext.SpanID()
					}
				}
				for _, ev := range span.Events() {
					if ev.Name == "cooldown.deferred" {
						deferred = true
					}
				}
			}
		}
		if !deferred || !cause.IsValid() || linked != cause {
			t.Fatalf("deferred=%v cause=%s linked=%s", deferred, cause, linked)
		}
	})
}
