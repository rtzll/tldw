package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// pathsCmd represents the paths command
var pathsCmd = &cobra.Command{
	Use:   "paths",
	Short: "Show paths used by the application",
	Example: `  # Show all application paths
  tldw paths`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Config directory: %s\n", config.ConfigDir)
		fmt.Printf("Data directory: %s\n", config.DataDir)
		fmt.Printf("Cache directory: %s\n", config.CacheDir)
		fmt.Printf("Transcripts directory: %s\n", config.TranscriptsDir)
	},
}

func init() {
	rootCmd.AddCommand(pathsCmd)
}
