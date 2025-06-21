package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version = "dev" // overridden at build time via -ldflags
	commit  = ""
	date    = ""
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Example: `  # Show version information
  tldw version`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("tldw v%s (commit: %s, built %s)\n", version, commit, date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
