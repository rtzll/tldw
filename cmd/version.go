package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const version = "0.1.0"

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Example: `  # Show version information
  tldw version`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("tldw v" + version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
