package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// pathsCmd represents the paths command
var pathsCmd = &cobra.Command{
	Use:     "paths",
	Args:    arguments(cobra.NoArgs),
	PreRunE: loadConfig,
	Short:   "Show paths used by the application",
	Example: `  # Show all application paths
  tldw paths`,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Config directory: %s\nData directory: %s\nCache directory: %s\nTranscripts directory: %s\n",
			config.ConfigDir, config.DataDir, config.CacheDir, config.TranscriptsDir)
		return err
	},
}

func init() {
	rootCmd.AddCommand(pathsCmd)
}
