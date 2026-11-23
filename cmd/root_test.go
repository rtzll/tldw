package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rtzll/tldw/internal"
)

func TestRootHelpUsesLowercaseProductName(t *testing.T) {
	if strings.Contains(rootCmd.Long, "TLDW (") {
		t.Fatalf("root help uses uppercase product name: %q", rootCmd.Long)
	}
}

func TestRootAcceptsBareVideoIDContainingCommandName(t *testing.T) {
	oldConfig, oldContext := config, rootCmd.Context()
	t.Cleanup(func() {
		config = oldConfig
		rootCmd.SetContext(oldContext)
	})
	config = &internal.Config{OpenAIAPIKey: "test", TLDRModel: internal.DefaultTLDRModel, Quiet: true}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rootCmd.SetContext(ctx)
	// The canceled request reaches the summary engine without making network calls.
	if err := rootCmd.RunE(rootCmd, []string{"abcpDE12345"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("valid video ID did not reach the engine: %v", err)
	}
}
