package ytdlp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestSelectDirectCaptionTrack(t *testing.T) {
	automatic := func(language string) tldw.CaptionTrack {
		return tldw.CaptionTrack{Language: language, Automatic: true, Direct: true}
	}
	manual := func(language string) tldw.CaptionTrack {
		return tldw.CaptionTrack{Language: language, Direct: true}
	}
	translated := tldw.CaptionTrack{Language: "en", Automatic: true}
	for _, tt := range []struct {
		name     string
		tracks   []tldw.CaptionTrack
		original string
		want     tldw.CaptionTrack
		ok       bool
	}{
		{"English original despite Arabic metadata", []tldw.CaptionTrack{automatic("ar-orig"), translated, automatic("en-orig")}, "ar", automatic("en-orig"), true},
		{"regional original", []tldw.CaptionTrack{translated, automatic("en-US-orig")}, "ar", automatic("en-US-orig"), true},
		{"other regional original", []tldw.CaptionTrack{translated, automatic("en-IN-orig")}, "hi", automatic("en-IN-orig"), true},
		{"original before manual", []tldw.CaptionTrack{manual("en-US"), automatic("en-orig")}, "en", automatic("en-orig"), true},
		{"manual regional only", []tldw.CaptionTrack{manual("en-US")}, "en", manual("en-US"), true},
		{"manual instead of translated same alias", []tldw.CaptionTrack{translated, manual("en")}, "de", manual("en"), true},
		{"direct automatic alias without orig suffix", []tldw.CaptionTrack{automatic("en")}, "en", automatic("en"), true},
		{"non-English original instead of translation", []tldw.CaptionTrack{translated, automatic("de-orig")}, "de", automatic("de-orig"), true},
		{"declared native language", []tldw.CaptionTrack{manual("fr"), manual("de")}, "de", manual("de"), true},
		{"undeclared native language", []tldw.CaptionTrack{automatic("fr-orig"), translated}, "", automatic("fr-orig"), true},
		{"translation only", []tldw.CaptionTrack{translated}, "de", tldw.CaptionTrack{}, false},
		{"missing tracks", nil, "en", tldw.CaptionTrack{}, false},
		{"live chat only", []tldw.CaptionTrack{manual("live_chat")}, "en", tldw.CaptionTrack{}, false},
		{"reject wildcard", []tldw.CaptionTrack{manual("en.*")}, "en", tldw.CaptionTrack{}, false},
		{"reject multiple languages", []tldw.CaptionTrack{manual("en,de")}, "en", tldw.CaptionTrack{}, false},
		{"reject path traversal", []tldw.CaptionTrack{manual("../../en")}, "en", tldw.CaptionTrack{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := selectCaptionTrack(tt.tracks, tt.original)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("selected %+v, %t; want %+v, %t", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestMetadataRetainsCaptionOrigin(t *testing.T) {
	yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
	yt.executor = &mockCommandRunner{output: []byte(`{
		"language":"ar", "channel":"Channel",
		"subtitles":{
			"en":[{"url":"https://www.youtube.com/api/timedtext?lang=en"}],
			"en-GB":[{"url":"https://www.youtube.com/api/timedtext?lang=fr&tlang=en-GB"}],
			"live_chat":[]
		},
		"automatic_captions":{
			"en":[{"url":"https://www.youtube.com/api/timedtext?lang=ar&tlang=en"}],
			"en-US-orig":[{"url":"https://www.youtube.com/api/timedtext?lang=en-US"}],
			"de":[{"url":"https://www.youtube.com/api/timedtext?lang=de"}],
			"fr-orig":[{"url":"https://www.youtube.com/api/timedtext?lang=de&tlang=fr"}],
			"unknown":[]
		}
	}`)}
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	metadata, err := yt.FetchMetadata(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	want := []tldw.CaptionTrack{
		{Language: "de", Automatic: true, Direct: true},
		{Language: "en", Direct: true}, {Language: "en", Automatic: true},
		{Language: "en-GB"},
		{Language: "en-US-orig", Automatic: true, Direct: true},
		{Language: "fr-orig", Automatic: true}, {Language: "unknown", Automatic: true},
	}
	if !reflect.DeepEqual(metadata.CaptionTracks, want) {
		t.Fatalf("tracks = %+v, want %+v", metadata.CaptionTracks, want)
	}
}

func TestDirectCaptionFormatsRejectsUnknownOrTranslatedURLs(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  any
		want bool
	}{
		{"direct", []any{map[string]any{"url": "https://www.youtube.com/api/timedtext?lang=en"}}, true},
		{"translated format among direct formats", []any{
			map[string]any{"url": "https://www.youtube.com/api/timedtext?lang=en"},
			map[string]any{"url": "https://www.youtube.com/api/timedtext?lang=de&tlang=en"},
		}, false},
		{"empty translation parameter", []any{map[string]any{"url": "https://www.youtube.com/api/timedtext?tlang="}}, false},
		{"missing formats", nil, false},
		{"missing URL", []any{map[string]any{"ext": "vtt"}}, false},
		{"relative URL", []any{map[string]any{"url": "/api/timedtext?lang=en"}}, false},
		{"malformed query", []any{map[string]any{"url": "https://www.youtube.com/api/timedtext?tlang=%ZZ"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := directCaptionFormats(tt.raw); got != tt.want {
				t.Fatalf("direct = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCaptionDownloadRequestsOneExactSource(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "automatic"}[automatic], func(t *testing.T) {
			track := tldw.CaptionTrack{Language: "en", Automatic: automatic, Direct: true}
			yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
			calls := 0
			yt.executor = commandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				calls++
				values := map[string]string{}
				flags := map[string]bool{}
				for i, arg := range args {
					flags[arg] = true
					if i+1 < len(args) {
						values[arg] = args[i+1]
					}
				}
				if values["--sub-langs"] != "en" || values["--convert-subs"] != "srt" || values["--sleep-subtitles"] != "5" || values["--extractor-args"] != youtubeExtractorPolicy {
					t.Fatalf("wrong exact download policy: %v", args)
				}
				if automatic != flags["--write-auto-subs"] || automatic != flags["--no-write-subs"] || automatic == flags["--write-subs"] || automatic == flags["--no-write-auto-subs"] {
					t.Fatalf("caption sources mixed: %v", args)
				}
				path := strings.ReplaceAll(values["-o"], "%(id)s", "dQw4w9WgXcQ") + ".en.srt"
				return nil, os.WriteFile(path, []byte("1\n00:00:00,000 --> 00:00:02,000\nHello\n"), 0o600)
			})
			ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
			got, err := yt.FetchCaptions(context.Background(), ref, &tldw.VideoMetadata{CaptionTracks: []tldw.CaptionTrack{track}})
			if err != nil || calls != 1 || got.CaptionTrack == nil || *got.CaptionTrack != track || got.Language != "en" {
				t.Fatalf("got %+v, err %v, calls %d", got, err, calls)
			}
		})
	}
}

func TestCaptionDownloadMissingExactTrackDoesNotBroaden(t *testing.T) {
	yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
	calls := 0
	yt.executor = commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil })
	track := tldw.CaptionTrack{Language: "en-orig", Automatic: true, Direct: true}
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	_, err := yt.FetchCaptions(context.Background(), ref, &tldw.VideoMetadata{CaptionTracks: []tldw.CaptionTrack{track}})
	var missing *tldw.CaptionTrackUnavailableError
	if calls != 1 || !errors.As(err, &missing) || !errors.Is(err, tldw.ErrCaptionsUnavailable) || missing.Track != track {
		t.Fatalf("calls %d, error %v", calls, err)
	}
}

func TestExistingSRTRespectsTrackAndPreservesUnrelatedFiles(t *testing.T) {
	data, cache := t.TempDir(), t.TempDir()
	old := filepath.Join(data, "dQw4w9WgXcQ.en.srt")
	original := filepath.Join(data, "dQw4w9WgXcQ.en-orig.srt")
	for path, text := range map[string]string{old: "Translated text", original: "Original text"} {
		if err := os.WriteFile(path, []byte("1\n00:00:00,000 --> 00:00:02,000\n"+text+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	yt := NewYouTube(data, cache, false, true)
	yt.executor = commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unexpected download")
		return nil, nil
	})
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	got, err := yt.FetchCaptions(context.Background(), ref, &tldw.VideoMetadata{CaptionTracks: []tldw.CaptionTrack{{Language: "en-orig", Automatic: true, Direct: true}}})
	if err != nil || got.Text != "Original text" {
		t.Fatalf("got %+v, err %v", got, err)
	}
	for _, path := range []string{old, original} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("persistent file removed: %v", err)
		}
	}
}

func TestManualTrackDoesNotReuseAmbiguousSRT(t *testing.T) {
	data := t.TempDir()
	old := filepath.Join(data, "dQw4w9WgXcQ.en.srt")
	contents := []byte("1\n00:00:00,000 --> 00:00:02,000\nOld translated text\n")
	if err := os.WriteFile(old, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	yt := NewYouTube(data, t.TempDir(), false, true)
	yt.executor = commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("download failed") })
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	_, err := yt.FetchCaptions(context.Background(), ref, &tldw.VideoMetadata{CaptionTracks: []tldw.CaptionTrack{{Language: "en", Direct: true}}})
	if !errors.Is(err, tldw.ErrDownloadFailed) {
		t.Fatalf("unexpected reuse: %v", err)
	}
	retained, err := os.ReadFile(old)
	if err != nil || string(retained) != string(contents) {
		t.Fatalf("old file damaged: %v", err)
	}
}
