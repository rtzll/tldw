package cmd

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetClaudeDesktopConfigPath(t *testing.T) {
	path, err := getClaudeDesktopConfigPath()
	if err != nil {
		t.Fatalf("getClaudeDesktopConfigPath() error = %v", err)
	}

	if path == "" {
		t.Error("getClaudeDesktopConfigPath() returned empty path")
	}

	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(path, "Claude") {
			t.Errorf("expected path to contain 'Claude', got %q", path)
		}
	case "linux":
		if !strings.Contains(path, ".config") {
			t.Errorf("expected path to contain '.config', got %q", path)
		}
	}
}
