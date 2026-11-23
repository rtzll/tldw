package cmd

import (
	"fmt"
	"strings"

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
	Args:  arguments(cobra.NoArgs),
	Short: "Print the version number",
	Example: `  # Show version information
  tldw version`,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), cmd.Root().Name(), cmd.Root().Version)
		return err
	},
}

func buildVersion() string {
	v := version
	if v != "dev" {
		v = "v" + strings.TrimPrefix(v, "v")
	}
	var details []string
	if commit != "" {
		details = append(details, "commit: "+commit)
	}
	if date != "" {
		details = append(details, "built "+date)
	}
	if len(details) > 0 {
		v += " (" + strings.Join(details, ", ") + ")"
	}
	return v
}

func init() {
	rootCmd.Version = buildVersion()
	rootCmd.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}
