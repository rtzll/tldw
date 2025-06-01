package cmd

import (
	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

// summarizeCmd represents the summarize command
var summarizeCmd = &cobra.Command{
	Use:   "summarize [YouTube URL or ID] [--fallback-whisper]",
	Short: "Generate summary from YouTube video",
	Example: `  # Generate summary from YouTube video
  tldw summarize "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw summarize tAP1eZYEuKA

  # Use specific OpenAI model
  tldw summarize tAP1eZYEuKA --model gpt-4o

  # Use custom prompt
  tldw summarize tAP1eZYEuKA --prompt "tldr: {{.Transcript}}"

  # Fallback to Whisper if no captions (costs money)
  tldw summarize tAP1eZYEuKA --fallback-whisper`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := internal.ValidateOpenAIRequirements(cmd, config); err != nil {
			return err
		}

		app := internal.NewApp(config)

		if err := internal.HandlePromptFlag(cmd, app); err != nil {
			return err
		}

		youtubeURL, _ := internal.ParseArg(args[0])
		fallbackWhisper, _ := cmd.Flags().GetBool("fallback-whisper")
		return app.SummarizeYouTube(cmd.Context(), youtubeURL, fallbackWhisper)
	},
}

func init() {
	internal.AddTranscriptionFlags(summarizeCmd)
	internal.AddOpenAIFlags(summarizeCmd)
	rootCmd.AddCommand(summarizeCmd)
}
