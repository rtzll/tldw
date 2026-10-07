package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestUpdateClaudeDesktopConfigPreservesUnmanagedSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_desktop_config.json")
	original := []byte(`{
		"preferences": {"theme": "dark", "id": 9007199254740993},
		"mcpServers": {
			"remote": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer test"}, "transport": "http"},
			"tldw": {"command": "old", "args": ["old"], "env": {"CUSTOM": "keep", "HOME": "old"}, "enabled": true}
		}
	}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	server := mcpServerConfig{Command: "/opt/homebrew/bin/tldw", Args: []string{"mcp"}, Env: map[string]string{"HOME": "/new/home"}}
	if err := updateClaudeDesktopConfig(path, server); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(original, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, after["preferences"], before["preferences"])
	var oldServers, servers map[string]json.RawMessage
	if err := json.Unmarshal(before["mcpServers"], &oldServers); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after["mcpServers"], &servers); err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, servers["remote"], oldServers["remote"])
	assertJSONEqual(t, servers["tldw"], []byte(`{"command":"/opt/homebrew/bin/tldw","args":["mcp"],"env":{"CUSTOM":"keep","HOME":"/new/home"},"enabled":true}`))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestUpdateClaudeDesktopConfigAddsServer(t *testing.T) {
	for _, original := range []string{`{}`, `{"mcpServers":null}`, `{"mcpServers":{"tldw":null}}`} {
		t.Run(original, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := updateClaudeDesktopConfig(path, mcpServerConfig{Command: "tldw", Args: []string{"mcp"}, Env: map[string]string{"HOME": "/home"}}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertJSONEqual(t, data, []byte(`{"mcpServers":{"tldw":{"command":"tldw","args":["mcp"],"env":{"HOME":"/home"}}}}`))
		})
	}
}

func TestUpdateClaudeDesktopConfigRejectsInvalidDataWithoutWriting(t *testing.T) {
	for _, original := range []string{`not json`, `null`, `[]`, `{"mcpServers":[]}`, `{"mcpServers":{"tldw":42}}`, `{"mcpServers":{"tldw":{"env":[]}}}`} {
		t.Run(original, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := updateClaudeDesktopConfig(path, mcpServerConfig{Command: "tldw"}); err == nil {
				t.Fatal("expected invalid configuration error")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != original {
				t.Fatalf("configuration changed on error: %s", data)
			}
		})
	}
}

func TestUpdateClaudeDesktopConfigPreservesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires special privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.json", path); err != nil {
		t.Fatal(err)
	}
	if err := updateClaudeDesktopConfig(path, mcpServerConfig{Command: "tldw"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(path); err != nil {
		t.Fatalf("configuration symlink was replaced: %v", err)
	}
}

func TestUpdateClaudeDesktopConfigRequiresExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if err := updateClaudeDesktopConfig(path, mcpServerConfig{Command: "tldw"}); err == nil {
		t.Fatal("expected missing configuration error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected configuration created: %v", err)
	}
}

func TestWriteClaudeConfigAtomicallyCleansUpOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeConfigAtomically(target, []byte(`{}`), 0o600); err == nil {
		t.Fatal("expected rename over directory to fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" || !entries[0].IsDir() {
		t.Fatalf("unexpected files after failed replacement: %v", entries)
	}
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var a, b any
	decode := func(data []byte, value *any) {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(value); err != nil {
			t.Fatal(err)
		}
	}
	decode(got, &a)
	decode(want, &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}
