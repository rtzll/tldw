package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

var (
	config *internal.Config
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "tldw <video-or-playlist-id-or-url>",
	Short: "Too Long; Didn't Watch - YouTube and X video summarizer",
	Long: `tldw (Too Long; Didn't Watch) summarizes YouTube videos, YouTube playlists, and single-video X posts using AI.

It uses existing captions first and offers paid OpenAI transcription when captions are unavailable.

Summaries use GPT-6 Luna (gpt-6-luna) by default.

Configuration can be provided via environment variables (e.g. OPENAI_API_KEY, TLDW_TLDR_MODEL, TLDW_PROMPT)
or by editing the config file at $XDG_CONFIG_HOME/tldw/config.toml.`,
	Example: `  # Summarize a YouTube video (default behavior)
  tldw "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw tAP1eZYEuKA

  # Summarize a YouTube playlist
  tldw "https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"
  tldw PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq

  # Explicitly select the default summary model
  tldw "https://youtu.be/tAP1eZYEuKA" --model gpt-6-luna

  # Use custom prompt for summary
  tldw tAP1eZYEuKA --prompt "tldr: {{.Transcript}}"

  # Use paid OpenAI transcription if captions are unavailable
  tldw "https://youtu.be/tAP1eZYEuKA" --allow-paid-transcription

  # Run quietly without progress bars or extra output
  tldw "https://youtu.be/tAP1eZYEuKA" --quiet`,
	PreRunE: loadConfig,
	Args:    arguments(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		ref, err := parseSummaryReference(args[0], cmd.Root().Commands())
		if err != nil {
			return usageError{err}
		}
		if err := validateOpenAIRequirements(cmd, config); err != nil {
			return err
		}

		if err := handlePromptFlag(cmd, config); err != nil {
			return err
		}
		app, err := newEngine(config, cmd.ErrOrStderr())
		if err != nil {
			return fmt.Errorf("building application: %w", err)
		}

		allowPaidTranscription, _ := cmd.Flags().GetBool("allow-paid-transcription")
		return runSummary(cmd, app, config, ref, allowPaidTranscription)
	},
}

func loadConfig(cmd *cobra.Command, _ []string) error {
	configFile, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return fmt.Errorf("reading config flag: %w", err)
	}
	config, err = internal.InitConfig(configFile)
	if err != nil {
		return fmt.Errorf("initializing configuration: %w", err)
	}
	if err := internal.EnsureDefaultConfig(config.ConfigDir); err != nil {
		return fmt.Errorf("ensuring default config: %w", err)
	}
	if err := internal.EnsureDefaultPrompt(config.ConfigDir); err != nil {
		return fmt.Errorf("ensuring default prompt: %w", err)
	}
	return applyOutputFlags(cmd, config)
}

// Execute handles process signals and translates the command outcome to an exit code.
func Execute() int {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	go func() {
		select {
		case sig := <-signals:
			signal.Stop(signals) // A second signal uses the process default.
			cancel(signalError{sig.(syscall.Signal)})
		case <-ctx.Done():
		}
	}()
	cmd, err := rootCmd.ExecuteContextC(ctx)
	if interrupted := context.Cause(ctx); interrupted != nil {
		err = interrupted
	}
	if err != nil {
		return reportFailure(cmd, err)
	}
	return 0
}

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SetFlagErrorFunc(flagError)
	addTranscriptionFlags(rootCmd)
	addOpenAIFlags(rootCmd)
	rootCmd.PersistentFlags().Bool("non-interactive", false, "Disable interactive prompts")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output for debugging")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Suppress progress bars and non-essential output")
	rootCmd.PersistentFlags().StringP("config", "c", "", "Config file (default is $XDG_CONFIG_HOME/tldw/config.toml)")

}
