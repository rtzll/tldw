package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitConfigDefaultsToGPT6Luna(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TLDW_TLDR_MODEL", "")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := InitConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.TLDRModel != "gpt-6-luna" {
		t.Fatalf("default TLDRModel = %q, want gpt-6-luna", config.TLDRModel)
	}
	if err := ValidateModel(config.TLDRModel); err != nil {
		t.Fatalf("default model failed validation: %v", err)
	}

	// The generated configuration must select the same default as Viper.
	configDir := t.TempDir()
	if err := EnsureDefaultConfig(configDir); err != nil {
		t.Fatal(err)
	}
	embedded, err := InitConfig(filepath.Join(configDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if embedded.TLDRModel != config.TLDRModel {
		t.Fatalf("embedded TLDRModel = %q, want %q", embedded.TLDRModel, config.TLDRModel)
	}
}

func TestInitConfigUsesExplicitFile(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TLDW_TLDR_MODEL", "")

	configPath := filepath.Join(t.TempDir(), "custom.toml")
	content := []byte(`
tldr_model = "gpt-5.4-mini"
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
	if config.TLDRModel != "gpt-5.4-mini" {
		t.Errorf("TLDRModel = %q, want gpt-5.4-mini", config.TLDRModel)
	}
	if config.TranscriptsDir != "/tmp/custom-transcripts" {
		t.Errorf("TranscriptsDir = %q, want /tmp/custom-transcripts", config.TranscriptsDir)
	}
	if config.SummaryTimeout != 45*time.Second {
		t.Errorf("SummaryTimeout = %v, want 45s", config.SummaryTimeout)
	}
}
