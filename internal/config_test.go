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

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Existing file
	existing := filepath.Join(tmpDir, "exists.txt")
	if err := os.WriteFile(existing, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	if !FileExists(existing) {
		t.Errorf("FileExists(%q) = false, want true", existing)
	}

	// Non-existing file
	nonExisting := filepath.Join(tmpDir, "does-not-exist.txt")
	if FileExists(nonExisting) {
		t.Errorf("FileExists(%q) = true, want false", nonExisting)
	}
}

func TestCleanupTempDir(t *testing.T) {
	t.Run("cleans up files", func(t *testing.T) {
		tmpDir := t.TempDir()
		f1 := filepath.Join(tmpDir, "file1.txt")
		f2 := filepath.Join(tmpDir, "file2.txt")
		os.WriteFile(f1, []byte("data"), 0644)
		os.WriteFile(f2, []byte("data"), 0644)

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

func TestIsLikelyYouTubeChannelHandle(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"valid brand", "mkbhd", true},
		{"valid with @", "@mkbhd", true},
		{"command-like", "help", false},
		{"common word", "test", false},
		{"too long no digits", "verylonghandlename", false},
		{"long with digits", "channel12345", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLikelyYouTubeChannelHandle(tt.s); got != tt.want {
				t.Errorf("isLikelyYouTubeChannelHandle(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}
