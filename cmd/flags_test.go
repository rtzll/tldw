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

func TestValidateOpenAIRequirementsModelSelection(t *testing.T) {
	tests := []struct {
		name        string
		configModel string
		flagModel   string
		wantModel   string
		wantError   bool
	}{
		{name: "default", configModel: internal.DefaultTLDRModel, wantModel: "gpt-6-luna"},
		{name: "configured override", configModel: "gpt-5.4-mini", wantModel: "gpt-5.4-mini"},
		{name: "flag selects default", configModel: "gpt-5.4-mini", flagModel: "gpt-6-luna", wantModel: "gpt-6-luna"},
		{name: "flag selects alternative", configModel: internal.DefaultTLDRModel, flagModel: "gpt-5.4-mini", wantModel: "gpt-5.4-mini"},
		{name: "flag overrides unsupported config", configModel: "gpt-4o", flagModel: "gpt-6-luna", wantModel: "gpt-6-luna"},
		{name: "unsupported config", configModel: "gpt-4o", wantError: true},
		{name: "unsupported flag", configModel: internal.DefaultTLDRModel, flagModel: "gpt-4o", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			addOpenAIFlags(cmd)
			if tt.flagModel != "" {
				if err := cmd.Flags().Set("model", tt.flagModel); err != nil {
					t.Fatal(err)
				}
			}
			config := &internal.Config{OpenAIAPIKey: "test-key", TLDRModel: tt.configModel}
			err := validateOpenAIRequirements(cmd, config)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateOpenAIRequirements() error = %v, want error: %v", err, tt.wantError)
			}
			if !tt.wantError && config.TLDRModel != tt.wantModel {
				t.Fatalf("selected model = %q, want %q", config.TLDRModel, tt.wantModel)
			}
		})
	}
}
