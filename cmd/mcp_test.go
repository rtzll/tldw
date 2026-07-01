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

func TestMCPHTTPDefaultPort(t *testing.T) {
	flag := mcpCmd.Flags().Lookup("port")
	if flag == nil {
		t.Fatal("mcp port flag is not registered")
	}

	if flag.DefValue != "8765" {
		t.Errorf("mcp port default = %q, want 8765", flag.DefValue)
	}
}

func TestMCPHTTPDefaultHost(t *testing.T) {
	flag := mcpCmd.Flags().Lookup("host")
	if flag == nil {
		t.Fatal("mcp host flag is not registered")
	}

	if flag.DefValue != "127.0.0.1" {
		t.Errorf("mcp host default = %q, want 127.0.0.1", flag.DefValue)
	}
}
