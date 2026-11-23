package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

// Re-exec the test binary through the production entry point to observe the
// actual process exit status and file descriptors. Each case gets fresh XDG dirs.
func TestMain(m *testing.M) {
	if os.Getenv("TLDW_TEST_CLI") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func cliCommand(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	c := exec.CommandContext(ctx, exe, args...)
	c.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TLDW_") && !strings.HasPrefix(kv, "OPENAI_") {
			c.Env = append(c.Env, kv)
		}
	}
	c.Env = append(c.Env, "TLDW_TEST_CLI=1", "OPENAI_API_KEY=test", "OPENAI_BASE_URL=http://127.0.0.1:1", "HOME="+dir,
		"XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"),
		"GORACE=atexit_sleep_ms=0")
	return c
}

func runCLI(t *testing.T, c *exec.Cmd) (stdout, stderr string, code int) {
	t.Helper()
	var out, diag bytes.Buffer
	c.Stdout, c.Stderr = &out, &diag
	err := c.Run()
	var exited *exec.ExitError
	if err != nil && !errors.As(err, &exited) {
		t.Fatal(err)
	}
	return out.String(), diag.String(), c.ProcessState.ExitCode()
}

func TestCLIErrors(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		code   int
		reason string
	}{
		{[]string{"stats", "--json", "--period", "never"}, 2, "invalid_arguments"},
		{[]string{"stats", "--unknown", "--json"}, 2, "invalid_arguments"},
		{[]string{"stats", "--json", "--config", "missing.toml"}, 1, "operation_failed"},
		{[]string{"metadata", "nonsense"}, 2, "invalid_arguments"},
		{[]string{"transcribe"}, 2, ""},
		{[]string{"mcp", "--transport", "unknown"}, 2, ""},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, diag, code := runCLI(t, cliCommand(t, t.TempDir(), tc.args...))
			if code != tc.code || out != "" || diag == "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out, diag)
			}
			if tc.reason != "" {
				var doc struct{ Status, Reason, Message string }
				if err := json.Unmarshal([]byte(diag), &doc); err != nil || doc.Reason != tc.reason || doc.Status == "" || doc.Message == "" {
					t.Fatalf("stderr=%q error=%v", diag, err)
				}
			}
		})
	}
}

func TestStatsDistinguishesEmptyFromPartial(t *testing.T) {
	dir := t.TempDir()
	out, diag, code := runCLI(t, cliCommand(t, dir, "stats", "--json"))
	if code != 0 || diag != "" || !strings.Contains(out, `"video_count": 0`) {
		t.Fatalf("empty: %d %q %q", code, out, diag)
	}
	cache := filepath.Join(dir, "data", "tldw", "transcripts")
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"dQw4w9WgXcQ.meta.json": `{"duration":42}`, "tAP1eZYEuKA.meta.json": "invalid"} {
		if err := os.WriteFile(filepath.Join(cache, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, diag, code = runCLI(t, cliCommand(t, dir, "stats", "--json"))
	var doc struct {
		Count      int     `json:"video_count"`
		Duration   float64 `json:"duration_seconds"`
		Incomplete bool
		Warnings   []string
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || code != 1 || diag != "" || doc.Count != 1 || doc.Duration != 42 || !doc.Incomplete || len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "tAP1eZYEuKA") {
		t.Fatalf("partial: %d %q %q", code, out, diag)
	}
	out, diag, code = runCLI(t, cliCommand(t, dir, "stats", "-q"))
	if code != 1 || !strings.Contains(out, "1 unique video") || !strings.Contains(diag, "Incomplete stats:") {
		t.Fatalf("text: %d %q %q", code, out, diag)
	}
}

func TestCLIFileExports(t *testing.T) {
	dir := t.TempDir()
	const id = "dQw4w9WgXcQ"
	cache := store.NewFile(filepath.Join(dir, "data", "tldw", "transcripts"))
	track := tldw.CaptionTrack{Language: "en", Direct: true}
	if err := cache.SaveMetadata(id, &tldw.VideoMetadata{Title: "Example", HasCaptions: true, CaptionTracks: []tldw.CaptionTrack{track}}); err != nil {
		t.Fatal(err)
	}
	transcript, err := tldw.NewCaptionTranscript(id, track, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.SaveTranscript(transcript); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"metadata", "transcribe"} {
		t.Run(name, func(t *testing.T) {
			out, diag, code := runCLI(t, cliCommand(t, dir, name, id, "-o", "-"))
			if code != 0 || diag != "" || out == "" {
				t.Fatalf("stdout: %d %q %q", code, out, diag)
			}
			if name == "metadata" && !json.Valid([]byte(out)) || name == "transcribe" && out != "hello\n" {
				t.Fatalf("unexpected stdout %q", out)
			}
			dest := filepath.Join(dir, name+".out")
			for i, flags := range [][]string{nil, nil, {"--force"}} {
				args := append([]string{name, id, "-o", dest}, flags...)
				stdout, stderr, exit := runCLI(t, cliCommand(t, dir, args...))
				if stdout != "" || i != 1 && (exit != 0 || stderr != "") || i == 1 && (exit != 1 || !strings.Contains(stderr, "--force")) {
					t.Fatalf("export %d: %d %q %q", i, exit, stdout, stderr)
				}
			}
			data, err := os.ReadFile(dest)
			if err != nil || string(data) != strings.TrimSuffix(out, "\n") {
				t.Fatalf("file %q, err %v", data, err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(dir, "-")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected file named dash: %v", err)
	}
}

func TestDiscoveryWorksBeforeConfiguration(t *testing.T) {
	dir := t.TempDir()
	var version string
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--version"}, "tldw "}, {[]string{"version"}, "tldw "},
		{[]string{"--help"}, "<video-or-playlist-id-or-url>"}, {[]string{"help", "transcribe"}, "<video-id-or-url>"},
		{[]string{"metadata", "--help"}, "--force"},
	} {
		args := append([]string{"--config", "unreadable.toml"}, tc.args...)
		out, diag, code := runCLI(t, cliCommand(t, dir, args...))
		if code != 0 || diag != "" || !strings.Contains(out, tc.want) {
			t.Fatalf("%v: %d %q %q", tc.args, code, out, diag)
		}
		if tc.want == "tldw " {
			if version != "" && version != out {
				t.Fatalf("version mismatch %q %q", version, out)
			}
			version = out
		}
	}
	for _, args := range [][]string{{"version", "extra"}, {"paths", "extra"}, {"mcp", "extra"}, {"mcp", "setup-claude", "extra"}} {
		out, _, code := runCLI(t, cliCommand(t, dir, args...))
		if out != "" || code != 2 {
			t.Fatalf("%v: %d %q", args, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "config")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discovery created a config directory: %v", err)
	}
}
