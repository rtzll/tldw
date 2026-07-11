package cmd

import "testing"

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
