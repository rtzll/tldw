package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal/tldw"
)

// metadataCmd represents the metadata command
var metadataCmd = &cobra.Command{
	Use:     "metadata <video-id-or-url>",
	PreRunE: loadConfig,
	Short:   "Get metadata from a YouTube video or single-video X post",
	Example: `  # Get metadata from YouTube video
  tldw metadata "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw metadata tAP1eZYEuKA

  # Save metadata to file
  tldw metadata tAP1eZYEuKA -o metadata.json

  # Format output as pretty JSON
  tldw metadata tAP1eZYEuKA --pretty`,
	Args: arguments(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newEngine(config, cmd.ErrOrStderr())
		if err != nil {
			return fmt.Errorf("building application: %w", err)
		}
		parsed, err := tldw.ParseVideoRef(args[0])
		if err != nil {
			return usageError{err}
		}
		return exportResult(cmd, func() ([]byte, error) {
			var metadata *tldw.VideoMetadata
			refresh, _ := cmd.Flags().GetBool("refresh")
			if refresh {
				metadata, err = app.RefreshMetadata(cmd.Context(), parsed)
			} else {
				metadata, err = app.MetadataFor(cmd.Context(), parsed)
			}
			if err != nil {
				return nil, err
			}
			pretty, _ := cmd.Flags().GetBool("pretty")
			if pretty {
				return json.MarshalIndent(metadata, "", "  ")
			}
			return json.Marshal(metadata)
		})
	},
}

func init() {
	addOutputFlags(metadataCmd)
	metadataCmd.Flags().Bool("refresh", false, "Fetch fresh metadata instead of using the cache")
	metadataCmd.Flags().Bool("pretty", false, "Format output as pretty JSON")
	rootCmd.AddCommand(metadataCmd)
}
