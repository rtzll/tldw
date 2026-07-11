package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
	"github.com/rtzll/tldw/internal/tldw"
)

var (
	config *internal.Config
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "tldw [URL]",
	Short: "Too Long; Didn't Watch - YouTube video summarizer",
	Long: `TLDW (Too Long; Didn't Watch) summarizes YouTube videos and playlists using AI.

It extracts transcripts directly from YouTube when available,
or processes the audio with Whisper when transcripts are unavailable.

The summary is generated using OpenAI's language models.

Configuration can be provided via environment variables (e.g. OPENAI_API_KEY, TLDW_TLDR_MODEL, TLDW_PROMPT)
or by editing the config file at $XDG_CONFIG_HOME/tldw/config.toml.`,
	Example: `  # Summarize a YouTube video (default behavior)
  tldw "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw tAP1eZYEuKA

  # Summarize a YouTube playlist
  tldw "https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"
  tldw PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq

  # Use a specific OpenAI model
  tldw "https://youtu.be/tAP1eZYEuKA" --model gpt-4o

  # Use custom prompt for summary
  tldw tAP1eZYEuKA --prompt "tldr: {{.Transcript}}"

  # Fallback to Whisper if no captions available (costs money)
  tldw "https://youtu.be/tAP1eZYEuKA" --fallback-whisper

  # Run quietly without progress bars or extra output
  tldw "https://youtu.be/tAP1eZYEuKA" --quiet`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		configFile, err := cmd.Root().PersistentFlags().GetString("config")
		if err != nil {
			return fmt.Errorf("reading config flag: %w", err)
		}
		config, err = internal.InitConfig(configFile)
		if err != nil {
			return fmt.Errorf("initializing configuration: %w", err)
		}
		if err := internal.EnsureDirs(config.ConfigDir, config.DataDir, config.CacheDir); err != nil {
			return fmt.Errorf("creating XDG directories: %w", err)
		}
		if err := internal.EnsureDefaultConfig(config.ConfigDir); err != nil {
			return fmt.Errorf("ensuring default config: %w", err)
		}
		if err := internal.EnsureDefaultPrompt(config.ConfigDir); err != nil {
			return fmt.Errorf("ensuring default prompt: %w", err)
		}
		return applyOutputFlags(cmd, config)
	},
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if suggestion, ok := commandSuggestion(args[0], cmd.Root().Commands()); ok {
			return fmt.Errorf("%s doesn't look like YouTube content; %s", args[0], suggestion)
		}
		if err := validateOpenAIRequirements(cmd, config); err != nil {
			return err
		}

		if err := handlePromptFlag(cmd, config); err != nil {
			return err
		}
		app, err := newEngine(config)
		if err != nil {
			return fmt.Errorf("building application: %w", err)
		}

		ref, err := tldw.ParseReference(args[0])
		if err != nil {
			return fmt.Errorf("invalid input %q: %w", args[0], err)
		}
		fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
		return runSummary(cmd.Context(), app, config, ref, fallbackWhisper)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd.SetContext(ctx)
	err := rootCmd.Execute()
	if ctx.Err() == nil || config == nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "\nReceived interrupt signal. Cleaning up and shutting down...")
	if cleanupErr := internal.CleanupTempDir(config.TempDir); cleanupErr != nil {
		return errors.Join(err, fmt.Errorf("cleaning up temporary files: %w", cleanupErr))
	}
	return err
}

func init() {
	rootCmd.SilenceUsage = true
	addTranscriptionFlags(rootCmd)
	addOpenAIFlags(rootCmd)
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output for debugging")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Suppress progress bars and non-essential output")
	rootCmd.PersistentFlags().StringP("config", "c", "", "Config file (default is $XDG_CONFIG_HOME/tldw/config.toml)")

}
