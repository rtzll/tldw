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
