package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

func TestApplyOutputFlags(t *testing.T) {
	tests := []struct {
		name        string
		verbose     bool
		quiet       bool
		wantVerbose bool
		wantQuiet   bool
	}{
		{name: "default"},
		{name: "verbose", verbose: true, wantVerbose: true},
		{name: "quiet", quiet: true, wantQuiet: true},
		{name: "quiet overrides verbose", verbose: true, quiet: true, wantQuiet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().Bool("verbose", tt.verbose, "")
			cmd.Flags().Bool("quiet", tt.quiet, "")
			config := &internal.Config{}

			if err := applyOutputFlags(cmd, config); err != nil {
				t.Fatalf("applyOutputFlags() error = %v", err)
			}
			if config.Verbose != tt.wantVerbose || config.Quiet != tt.wantQuiet {
				t.Fatalf("applyOutputFlags() = verbose:%v quiet:%v, want verbose:%v quiet:%v", config.Verbose, config.Quiet, tt.wantVerbose, tt.wantQuiet)
			}
		})
	}
}
