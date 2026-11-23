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
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "Config directory: %s\n", config.ConfigDir)
		fmt.Fprintf(cmd.OutOrStdout(), "Data directory: %s\n", config.DataDir)
		fmt.Fprintf(cmd.OutOrStdout(), "Cache directory: %s\n", config.CacheDir)
		fmt.Fprintf(cmd.OutOrStdout(), "Transcripts directory: %s\n", config.TranscriptsDir)
	},
}

func init() {
	rootCmd.AddCommand(pathsCmd)
}
