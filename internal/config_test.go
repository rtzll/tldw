package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitConfigUsesExplicitFile(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "custom.toml")
	content := []byte(`
tldr_model = "gpt-5-mini"
transcripts_dir = "/tmp/custom-transcripts"
summary_timeout = "45s"
`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	config, err := InitConfig(configPath)
	if err != nil {
		t.Fatalf("InitConfig() error = %v", err)
	}
	if config.TLDRModel != "gpt-5-mini" {
		t.Errorf("TLDRModel = %q, want gpt-5-mini", config.TLDRModel)
	}
	if config.TranscriptsDir != "/tmp/custom-transcripts" {
		t.Errorf("TranscriptsDir = %q, want /tmp/custom-transcripts", config.TranscriptsDir)
	}
	if config.SummaryTimeout != 45*time.Second {
		t.Errorf("SummaryTimeout = %v, want 45s", config.SummaryTimeout)
	}
}

func TestCleanupTempDir(t *testing.T) {
	t.Run("cleans up files", func(t *testing.T) {
		tmpDir := t.TempDir()
		f1 := filepath.Join(tmpDir, "file1.txt")
		f2 := filepath.Join(tmpDir, "nested", "file2.txt")
		if err := os.MkdirAll(filepath.Dir(f2), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
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
