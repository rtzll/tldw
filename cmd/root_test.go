package cmd

import (
	"strings"
	"testing"
)

func TestRootHelpUsesLowercaseProductName(t *testing.T) {
	if strings.Contains(rootCmd.Long, "TLDW (") {
		t.Fatalf("root help uses uppercase product name: %q", rootCmd.Long)
	}
}
