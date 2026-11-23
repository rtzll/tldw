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
