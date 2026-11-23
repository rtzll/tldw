package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceCleansUpOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Replace(target, []byte(`{}`), 0o600); err == nil {
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

func TestCreatePreservesExistingContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := Create(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Create(path, []byte("second"), 0600); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second create: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatalf("got %q, %v", data, err)
	}
}
