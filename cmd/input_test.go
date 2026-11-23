package cmd

import (
	"strings"
	"testing"
)

func TestCommandSuggestionRecognizesCommandTypos(t *testing.T) {
	commands := rootCmd.Commands()
	suggestion, ok := commandSuggestion("transcrib", commands)
	if !ok || suggestion != "did you mean: transcribe" {
		t.Fatalf("commandSuggestion(transcrib) = %q, %v", suggestion, ok)
	}
	if suggestion, ok := commandSuggestion("metadat", commands); !ok || suggestion != "did you mean: metadata" {
		t.Fatalf("commandSuggestion(metadat) = %q, %v", suggestion, ok)
	}
	if _, ok := commandSuggestion("mkbhd", commands); ok {
		t.Fatal("commandSuggestion(mkbhd) classified a channel handle as a command")
	}
	if _, ok := commandSuggestion("@mkbhd", commands); ok {
		t.Fatal("commandSuggestion(@mkbhd) classified an explicit channel handle as a command")
	}
}

func TestSummaryReferencePrecedesTypoSuggestions(t *testing.T) {
	commands := rootCmd.Commands()
	for _, input := range []string{"abcpDE12345", "PLcp12345678901234", "https://youtu.be/abcpDE12345"} {
		if _, err := parseSummaryReference(input, commands); err != nil {
			t.Errorf("rejected valid reference %q: %v", input, err)
		}
	}
	if _, err := parseSummaryReference("transcrib", commands); err == nil || !strings.Contains(err.Error(), "did you mean: transcribe") {
		t.Errorf("missing suggestion for unsupported summary input: %v", err)
	}
	if _, err := parseSummaryReference("@mkbhd", commands); err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Errorf("unexpected channel handling: %v", err)
	}
}
