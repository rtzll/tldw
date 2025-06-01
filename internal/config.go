package internal

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/lrstanley/go-ytdlp"
	"github.com/spf13/viper"
)

// CommandRunner executes external commands
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultCommandRunner implements CommandRunner
type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// Config holds application settings
type Config struct {
	// User configurable settings
	TLDRModel      string
	TranscriptsDir string
	SummaryTimeout time.Duration
	WhisperTimeout time.Duration
	Verbose        bool
	OpenAIAPIKey   string
	Prompt         string

	// Fixed XDG paths (not configurable)
	ConfigDir string
	DataDir   string
	CacheDir  string
	TempDir   string
}

//go:embed config.toml prompt.txt
var defaultFS embed.FS

// WhisperLimit is the maximum file size accepted by OpenAI's Whisper API (25 MiB)
const WhisperLimit int64 = 25 << 20

// ensureDefaultFile checks if a file exists in the specified directory
// and creates it from the embedded default if it doesn't exist
func ensureDefaultFile(configDir, embedFilename, description string) error {
	filePath := filepath.Join(configDir, embedFilename)

	// Check if file already exists
	if FileExists(filePath) {
		return nil
	}

	// Make sure the config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Read the embedded default file
	defaultContent, err := defaultFS.ReadFile(embedFilename)
	if err != nil {
		return fmt.Errorf("reading embedded default %s: %w", description, err)
	}

	// Write the default file to the specified directory
	if err := os.WriteFile(filePath, defaultContent, 0644); err != nil {
		return fmt.Errorf("writing default %s: %w", description, err)
	}

	fmt.Printf("Created default %s at %s\n", description, filePath)
	return nil
}

// EnsureDefaultConfig checks if a config file exists in the XDG config directory
// and creates it from the embedded default if it doesn't exist
func EnsureDefaultConfig(configDir string) error {
	return ensureDefaultFile(configDir, "config.toml", "configuration")
}

// EnsureDefaultPrompt checks if a prompt.txt file exists in the XDG config directory
// and creates it from the embedded default if it doesn't exist
func EnsureDefaultPrompt(configDir string) error {
	return ensureDefaultFile(configDir, "prompt.txt", "prompt template")
}

// InitConfig initializes Viper and loads configuration
func InitConfig() *Config {
	// Ensure yt-dlp is installed
	ytdlp.MustInstall(context.Background(), nil)

	// XDG standard directories
	configDir := filepath.Join(xdg.ConfigHome, "tldw")
	dataDir := filepath.Join(xdg.DataHome, "tldw")
	cacheDir := filepath.Join(xdg.CacheHome, "tldw")

	// directories for transcripts and temp files
	transcriptsDir := filepath.Join(dataDir, "transcripts")
	tempDir := filepath.Join(cacheDir, "temp_chunks")

	// Initialize viper
	v := viper.New()

	// Set default values for configurable settings
	v.SetDefault("tldr_model", "gpt-4o-mini")
	v.SetDefault("transcripts_dir", transcriptsDir)
	v.SetDefault("summary_timeout", 2*time.Minute)
	v.SetDefault("whisper_timeout", 10*time.Minute)
	v.SetDefault("verbose", false)
	v.SetDefault("prompt", "") // if empty will use default prompt template

	// Set config name and paths
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(configDir)
	v.AddConfigPath(".")

	// Environment variables
	v.SetEnvPrefix("TLDW")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("_", "_"))

	// Special case for OpenAI API Key - check both Viper and direct env var
	_ = v.BindEnv("openai_api_key", "OPENAI_API_KEY")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Warning: Error reading config file: %v\n", err)
		}
	}

	// Create config struct from viper
	config := &Config{
		// User configurable settings
		TLDRModel:      v.GetString("tldr_model"),
		TranscriptsDir: v.GetString("transcripts_dir"),
		SummaryTimeout: v.GetDuration("summary_timeout"),
		WhisperTimeout: v.GetDuration("whisper_timeout"),
		Verbose:        v.GetBool("verbose"),
		OpenAIAPIKey:   v.GetString("openai_api_key"),
		Prompt:         v.GetString("prompt"),

		// Fixed XDG paths
		ConfigDir: configDir,
		DataDir:   dataDir,
		CacheDir:  cacheDir,
		TempDir:   tempDir,
	}

	if config.Verbose {
		fmt.Printf("Using config file: %s\n", v.ConfigFileUsed())
	}

	return config
}
