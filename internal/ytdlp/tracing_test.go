package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	for _, tc := range []struct {
		name           string
		emptyDownloads int
	}{{"recovers", 1}, {"exhausted", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				recorder := tracetest.NewSpanRecorder()
				provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
				ctx, root := provider.Tracer("test").Start(t.Context(), "request")
				defer root.End()
				ref, _ := tldw.ParseVideoRef("v9gt7u_7nWQ")
				disk := store.NewFile(dir)
				err := disk.SaveMetadata(ref.CacheKey(), &tldw.VideoMetadata{
					Channel: "fixture", HasCaptions: true, CaptionLanguages: []string{"en-orig"},
					CaptionTracks: []tldw.CaptionTrack{{Language: "en-orig", Automatic: true, Direct: true}},
				})
				if err != nil {
					t.Fatal(err)
				}
				cause := errors.New("WARNING: private diagnostic\nERROR: Did not get any data blocks\nWARNING: Skipping embedding en-orig subtitle because the file is missing")
				video := New(dir, dir, false, true)
				calls := 0
				video.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
					calls++
					if !slices.Contains(args, "--write-auto-subs") {
						t.Fatalf("unexpected download: %v", args)
					}
					if calls <= tc.emptyDownloads {
						return nil, cause
					}
					path := args[slices.Index(args, "-o")+1] + ".en-orig.srt"
					return nil, os.WriteFile(path, []byte("1\n00:00:00,000 --> 00:00:01,000\nRecovered caption\n"), 0600)
				})
				engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: disk, AI: unusedDependencies{}, Prompts: unusedDependencies{}})
				if err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				got, err := engine.Transcript(ctx, ref, tldw.CaptionsThenPaid)
				if calls != 2 || time.Since(start) != time.Second {
					t.Fatalf("attempts=%d elapsed=%v", calls, time.Since(start))
				}
				if tc.emptyDownloads == 1 {
					if err != nil || got == nil || got.PlainText() != "Recovered caption" {
						t.Fatalf("recovered transcript=%+v err=%v", got, err)
					}
				} else if !errors.Is(err, cause) || !errors.Is(err, tldw.ErrDownloadFailed) || tldw.CanOfferPaidTranscription(err) ||
					!strings.Contains(err.Error(), "after two attempts") || !strings.Contains(err.Error(), "Try again later") || strings.Contains(err.Error(), "private diagnostic") {
					t.Fatalf("unexpected terminal error: %v", err)
				}
				var emptySpans int
				for _, span := range recorder.Ended() {
					if span.Name() == "yt-dlp.captions" {
						for _, kv := range span.Attributes() {
							if kv.Key == "error.type" && kv.Value.AsString() == "empty_caption_response" {
								emptySpans++
							}
						}
					}
				}
				if emptySpans != tc.emptyDownloads {
					t.Fatalf("empty download spans=%d, want %d", emptySpans, tc.emptyDownloads)
				}
			})
		})
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
		return []byte("private transcript body"), errors.New("WARNING: HTTP Error 503: retrying\nERROR: Unable to download video subtitles: HTTP Error 403: Forbidden https://example.invalid/?token=do-not-export")
	})
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	_, err = client.downloadCaptions(ctx, ref, directTrack{Language: "en-orig", Automatic: true})
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
