//go:build linux || darwin

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/creack/pty"
	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

func fakeDownloader(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const script = `#!/bin/sh
case " $* " in
  *" --extract-audio "*)
    while [ "$1" != "-o" ]; do shift; done
    printf 'test audio' > "${2%.*}.mp3" ;;
  *" --flat-playlist "*)
    echo '{"title":"Playlist","entries":[{"id":"dQw4w9WgXcQ"},{"id":"tAP1eZYEuKA"}]}' ;;
  *" --dump-single-json "*)
    echo '{"title":"Example video","channel":"Example channel","duration":15}' ;;
  *) exit 20 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Keep stdout redirected while driving stdin and stderr through a real PTY.
func runPrompt(t *testing.T, c *exec.Cmd, pipedInput bool, respond func(int, *os.File, *os.Process)) (string, string, int, int) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close() }()
	var stdout bytes.Buffer
	c.Stdout, c.Stdin, c.Stderr = &stdout, slave, slave
	if pipedInput {
		c.Stdin = strings.NewReader("yes\n")
	}
	if err = c.Start(); err != nil {
		_ = slave.Close()
		t.Fatal(err)
	}
	_ = slave.Close()
	reader := bufio.NewReader(master)
	var diag strings.Builder
	prompts := 0
	for {
		b, err := reader.ReadByte()
		if errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		diag.WriteByte(b)
		if strings.HasSuffix(diag.String(), "(y/N): ") {
			prompts++
			respond(prompts, master, c.Process)
		}
	}
	err = c.Wait()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return stdout.String(), diag.String(), c.ProcessState.ExitCode(), prompts
}

func TestTerminalPaidConsent(t *testing.T) {
	const video = "dQw4w9WgXcQ"
	const playlist = "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"
	downloader := fakeDownloader(t)
	for _, tc := range []struct {
		name, ref, ci                     string
		captionedID                       string
		flags, answers                    []string
		piped                             bool
		signal                            syscall.Signal
		code, prompts, uploads, summaries int
		diagnostic                        string
	}{
		{name: "redirected stdout", ref: video, answers: []string{"yes"}, prompts: 1, uploads: 1, summaries: 1},
		{name: "piped yes", ref: video, piped: true, code: 2, diagnostic: "--allow-paid-transcription"},
		{name: "disabled", ref: video, flags: []string{"--non-interactive"}, code: 2, diagnostic: "--allow-paid-transcription"},
		{name: "CI terminal", ref: video, ci: "true", code: 2, diagnostic: "--allow-paid-transcription"},
		{name: "explicit paid in CI", ref: video, flags: []string{"--allow-paid-transcription"}, ci: "true", uploads: 1, summaries: 1},
		{name: "declined", ref: video, answers: []string{"no"}, code: 130, prompts: 1},
		{name: "interrupt", ref: video, signal: syscall.SIGINT, code: 130, prompts: 1},
		{name: "terminate", ref: video, signal: syscall.SIGTERM, code: 143, prompts: 1},
		{name: "hangup", ref: video, signal: syscall.SIGHUP, code: 129, prompts: 1},
		{name: "partial playlist", ref: playlist, answers: []string{"y", "no"}, code: 1, prompts: 2, uploads: 1, summaries: 1, diagnostic: "tAP1eZYEuKA"},
		{name: "declined playlist", ref: playlist, answers: []string{"no", "no"}, code: 130, prompts: 2, diagnostic: "paid transcription declined"},
		{name: "playlist in CI", ref: playlist, ci: "1", code: 2, diagnostic: "--allow-paid-transcription"},
		{name: "explicit paid playlist in CI", ref: playlist, ci: "1", flags: []string{"--allow-paid-transcription"}, uploads: 2, summaries: 1},
		{name: "captions first in CI", ref: playlist, ci: "1", captionedID: video, code: 1, summaries: 1, diagnostic: "tAP1eZYEuKA"},
		{name: "captions last with prompts disabled", ref: playlist, flags: []string{"--non-interactive"}, captionedID: "tAP1eZYEuKA", code: 1, summaries: 1, diagnostic: video},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.captionedID != "" {
				transcript, err := tldw.NewCaptionTranscript(tc.captionedID, tldw.CaptionTrack{Language: "en", Direct: true}, "Available playlist captions", nil)
				if err != nil {
					t.Fatal(err)
				}
				cache := store.NewFile(filepath.Join(dir, "data", "tldw", "transcripts"))
				if err := cache.SaveTranscript(transcript); err != nil {
					t.Fatal(err)
				}
			}
			var uploads, summaries atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/audio/transcriptions":
					uploads.Add(1)
					_, _ = io.WriteString(w, `{"text":"Example transcript"}`)
				case "/chat/completions":
					summaries.Add(1)
					if tc.captionedID != "" {
						body, err := io.ReadAll(r.Body)
						if err != nil || !bytes.Contains(body, []byte("Available playlist captions")) {
							t.Errorf("summary request omitted available captions: %q, %v", body, err)
						}
					}
					_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"## Summary"}}]}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			c := cliCommand(t, dir, append([]string{tc.ref, "--quiet"}, tc.flags...)...)
			c.Env = append(c.Env, "PATH="+downloader, "OPENAI_BASE_URL="+server.URL, "CI="+tc.ci)
			out, diag, code, count := runPrompt(t, c, tc.piped, func(n int, master *os.File, process *os.Process) {
				switch {
				case tc.signal != 0:
					if err := process.Signal(tc.signal); err != nil {
						t.Fatal(err)
					}
				case n <= len(tc.answers):
					if _, err := io.WriteString(master, tc.answers[n-1]+"\n"); err != nil {
						t.Fatal(err)
					}
				default:
					_ = process.Kill()
				}
			})
			want := ""
			if tc.summaries > 0 {
				want = "## Summary\n"
			}
			if code != tc.code || count != tc.prompts || uploads.Load() != int32(tc.uploads) || summaries.Load() != int32(tc.summaries) || out != want || !strings.Contains(diag, tc.diagnostic) {
				t.Fatalf("code=%d prompts=%d uploads=%d summaries=%d stdout=%q stderr=%q", code, count, uploads.Load(), summaries.Load(), out, diag)
			}
		})
	}
}

func TestPlaylistWithCorruptCacheAndMissingCaptionsReportsFailure(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "data", "tldw", "transcripts")
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "dQw4w9WgXcQ.captions.transcript.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	c := cliCommand(t, dir, "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", "--non-interactive")
	c.Env = append(c.Env, "PATH="+fakeDownloader(t))
	out, diag, code := runCLI(t, c)
	if code != 1 || out != "" || !strings.Contains(diag, "parsing structured transcript") || strings.Contains(diag, "--allow-paid-transcription") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, diag)
	}
}

func TestVerboseMetadataPipesOneJSONDocument(t *testing.T) {
	c := cliCommand(t, t.TempDir(), "metadata", "--verbose", "dQw4w9WgXcQ")
	c.Env = append(c.Env, "PATH="+fakeDownloader(t))
	out, diag, code := runCLI(t, c)
	var result struct{ Title string }
	if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || result.Title != "Example video" || !strings.Contains(diag, "Extracting video metadata") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, diag)
	}
}

func TestRedirectedStderrDisablesConsentWithTerminalInput(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close(); _ = slave.Close() }()
	if _, err := io.WriteString(master, "yes\n"); err != nil {
		t.Fatal(err)
	}
	c := cliCommand(t, t.TempDir(), "dQw4w9WgXcQ")
	c.Env = append(c.Env, "PATH="+fakeDownloader(t), "CI=false")
	c.Stdin = slave
	out, diag, code := runCLI(t, c)
	if code != 2 || out != "" || strings.Contains(diag, "(y/N)") || !strings.Contains(diag, "--allow-paid-transcription") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, diag)
	}
}
