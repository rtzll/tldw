package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirs(t *testing.T) {
	tmpDir := t.TempDir()

	newDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := EnsureDirs(newDir); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("expected dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected path to be a directory")
	}

	// Should be idempotent
	if err := EnsureDirs(newDir); err != nil {
		t.Fatalf("EnsureDirs() idempotent error = %v", err)
	}
}

func TestCleanupTempDir(t *testing.T) {
	t.Run("cleans up files", func(t *testing.T) {
		tmpDir := t.TempDir()
		f1 := filepath.Join(tmpDir, "file1.txt")
		f2 := filepath.Join(tmpDir, "file2.txt")
		if err := os.WriteFile(f1, []byte("data"), 0644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", f1, err)
		}
		if err := os.WriteFile(f2, []byte("data"), 0644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", f2, err)
		}

		if err := CleanupTempDir(tmpDir); err != nil {
			t.Fatalf("CleanupTempDir() error = %v", err)
		}

		if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
			t.Error("expected temp dir to be removed")
		}
	})

	t.Run("non-existent dir", func(t *testing.T) {
		err := CleanupTempDir(filepath.Join(t.TempDir(), "does-not-exist"))
		if err != nil {
			t.Errorf("CleanupTempDir() error = %v", err)
		}
	})
}
