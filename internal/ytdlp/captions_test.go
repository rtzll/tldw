package ytdlp

import (
	"context"
	"errors"
	"github.com/rtzll/tldw/internal/process"
	"github.com/rtzll/tldw/internal/tldw"
	"os"
	"path/filepath"
	"testing"
)

func TestFindExistingTranscriptReturnsDirectoryErrors(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(cachePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	yt := NewYouTube(t.TempDir(), cachePath, false, true)

	if _, err := yt.findExistingTranscript("dQw4w9WgXcQ", "en-orig"); err == nil {
		t.Fatal("findExistingTranscript() ignored an unreadable cache directory")
	}
}

func TestExtractCaptionLanguages(t *testing.T) {
	tests := []struct {
		name         string
		subtitles    map[string]any
		autoCaptions map[string]any
		want         []string
	}{
		{"empty", nil, nil, nil},
		{"manual only", map[string]any{"en": nil, "de": nil}, nil, []string{"de", "en"}},
		{"auto only", nil, map[string]any{"en": nil, "fr": nil}, []string{"en", "fr"}},
		{"combined", map[string]any{"en": nil}, map[string]any{"de": nil}, []string{"de", "en"}},
		{"skip live_chat", map[string]any{"en": nil, "live_chat": nil}, nil, []string{"en"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCaptionLanguages(tt.subtitles, tt.autoCaptions)
			if len(got) != len(tt.want) {
				t.Errorf("extractCaptionLanguages() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractCaptionLanguages() = %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}

func TestCaptionErrorsPreserveCauseAndStopRetries(t *testing.T) {
	for _, tt := range []struct {
		name   string
		stdout string
		cause  error
		stderr string
		want   error
	}{
		{name: "cancel", cause: context.Canceled, want: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "rate limit on stderr", cause: errors.New("exit status 1"), stderr: "ERROR: HTTP Error 429: Too Many Requests", want: tldw.ErrRateLimited},
		{name: "rate limit on stdout", cause: errors.New("exit status 1"), stdout: "HTTP Error 429", want: tldw.ErrRateLimited},
	} {
		t.Run(tt.name, func(t *testing.T) {
			yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
			calls := 0
			commandErr := &process.CommandError{Name: "yt-dlp", Stderr: tt.stderr, Err: tt.cause}
			yt.executor = commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
				calls++
				return []byte(tt.stdout), commandErr
			})
			ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
			_, err := yt.FetchCaptions(context.Background(), ref, &tldw.VideoMetadata{CaptionTracks: []tldw.CaptionTrack{{Language: "en", Direct: true}}})
			var got *process.CommandError
			if !errors.Is(err, tt.want) || !errors.As(err, &got) || got != commandErr {
				t.Fatalf("error chain lost: %v", err)
			}
			if calls != 1 {
				t.Fatalf("terminal error made %d attempts", calls)
			}
		})
	}
}

func TestCaptionDownloadPreservesErrorChain(t *testing.T) {
	yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
	calls := 0
	cause := errors.New("network failure")
	yt.executor = commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, cause })
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	_, err := yt.FetchCaptions(context.Background(), ref, &tldw.VideoMetadata{CaptionTracks: []tldw.CaptionTrack{{Language: "en", Direct: true}}})
	if calls != 1 || !errors.Is(err, cause) || !errors.Is(err, tldw.ErrDownloadFailed) {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestCanceledDownloadDoesNotAcceptPartialFiles(t *testing.T) {
	dir := t.TempDir()
	yt := NewYouTube(t.TempDir(), dir, false, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	yt.executor = commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		if err := os.WriteFile(filepath.Join(dir, "partial.srt"), []byte("partial"), 0644); err != nil {
			t.Fatal(err)
		}
		cancel()
		return nil, errors.New("process exited")
	})
	_, files, err := yt.runCaptionDownload(ctx, nil, filepath.Join(dir, "*.srt"))
	if !errors.Is(err, context.Canceled) || len(files) != 0 {
		t.Fatalf("accepted canceled partial download: files=%v err=%v", files, err)
	}
}
