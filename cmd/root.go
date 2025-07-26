package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/rtzll/tldw/internal"
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
  tldw "https://youtu.be/tAP1eZYEuKA" --fallback-whisper`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return internal.HandleVerboseFlag(cmd, config)
	},
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := internal.ValidateOpenAIRequirements(cmd, config); err != nil {
			return err
		}

		app := internal.NewApp(config)
		if err := internal.HandlePromptFlag(cmd, app); err != nil {
			return err
		}

		// Validate argument before processing
		arg := args[0]
		if internal.IsLikelyCommand(arg) {
			// Check if it's a valid YouTube video ID or playlist ID
			if !internal.IsValidYouTubeID(arg) && !internal.IsValidPlaylistID(arg) {
				// Check if it's similar to any available commands
				availableCommands := []string{"mcp", "transcribe", "version", "paths", "help"}
				var suggestions []string
				for _, cmdName := range availableCommands {
					if strings.Contains(cmdName, arg) || (len(arg) <= len(cmdName) && strings.Contains(arg, cmdName[:len(arg)])) {
						suggestions = append(suggestions, cmdName)
					}
				}

				if len(suggestions) > 0 {
					return fmt.Errorf("%s doesn't look like a YouTube URL, video ID, or playlist ID; did you mean %s", arg, strings.Join(suggestions, ", "))
				}
				return fmt.Errorf("%s doesn't look like a YouTube URL, video ID, or playlist ID; use --help", arg)
			}
		}

		youtubeURL, _ := internal.ParseArg(args[0])
		fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
		return app.SummarizeYouTube(cmd.Context(), youtubeURL, fallbackWhisper)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	// Create a cancellable context for the entire application
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize configuration with Viper
	var err error
	config, err = internal.InitConfig()
	if err != nil {
		return fmt.Errorf("initializing configuration: %w", err)
	}

	// Ensure XDG directories exist
	if err := internal.EnsureDirs(config.ConfigDir, config.DataDir, config.CacheDir); err != nil {
		return fmt.Errorf("creating XDG directories: %w", err)
	}

	// Ensure default config exists in XDG config directory
	if err := internal.EnsureDefaultConfig(config.ConfigDir); err != nil {
		return fmt.Errorf("ensuring default config: %w", err)
	}

	// Ensure default prompt exists in XDG config directory
	if err := internal.EnsureDefaultPrompt(config.ConfigDir); err != nil {
		return fmt.Errorf("ensuring default prompt: %w", err)
	}

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Handle shutdown signal in a separate goroutine
	go func() {
		<-sigCh
		fmt.Println("\nReceived interrupt signal. Cleaning up and shutting down...")

		// Cancel the main context to signal all operations to stop
		cancel()

		// Create a context with timeout for cleanup operations
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanupCancel()

		// Run cleanup with timeout context
		cleanupDone := make(chan struct{})
		go func() {
			if err := internal.CleanupTempDir(config.TempDir); err != nil {
				fmt.Fprintf(os.Stderr, "failed to clean up temporary files: %v\n", err)
			}
			close(cleanupDone)
		}()

		// Wait for either cleanup to complete or timeout
		select {
		case <-cleanupDone:
			// Cleanup completed successfully
		case <-cleanupCtx.Done():
			// Timeout occurred
			fmt.Fprintln(os.Stderr, "cleanup timed out, forcing exit")
		}

		// Exit the program
		os.Exit(0)
	}()

	// Set context on root command
	rootCmd.SetContext(ctx)
	return rootCmd.Execute()
}

func init() {
	rootCmd.SilenceUsage = true
	internal.AddTranscriptionFlags(rootCmd)
	internal.AddOpenAIFlags(rootCmd)
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output for debugging")
	rootCmd.PersistentFlags().StringP("config", "c", "", "Config file (default is $XDG_CONFIG_HOME/tldw/config.toml)")

	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))
	_ = viper.BindPFlag("tldr_model", rootCmd.Flags().Lookup("model"))
	_ = viper.BindPFlag("prompt", rootCmd.Flags().Lookup("prompt"))
}
