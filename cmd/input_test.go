package cmd

import "testing"

func TestCommandSuggestionRecognizesCommandTypos(t *testing.T) {
	suggestion, ok := commandSuggestion("transcrib")
	if !ok || suggestion != "did you mean: transcribe" {
		t.Fatalf("commandSuggestion(transcrib) = %q, %v", suggestion, ok)
	}
	if _, ok := commandSuggestion("mkbhd"); ok {
		t.Fatal("commandSuggestion(mkbhd) classified a channel handle as a command")
	}
	if _, ok := commandSuggestion("@mkbhd"); ok {
		t.Fatal("commandSuggestion(@mkbhd) classified an explicit channel handle as a command")
	}
}
