package cmd

import (
	"strings"
	"testing"
)

func TestPaidConfirmationRequiresAnExplicitYesForEachItem(t *testing.T) {
	answers := []struct {
		input string
		want  bool
	}{
		{"y", true}, {"  YeS  ", true}, {"no", false}, {"yesterday", false},
		{"yes please", false}, {"", false}, {"Y", true},
	}
	var input strings.Builder
	for _, answer := range answers {
		input.WriteString(answer.input + "\n")
	}
	confirm := confirmationPrompt(strings.NewReader(input.String()))
	for _, answer := range answers {
		if got := confirm("Use paid transcription?"); got != answer.want {
			t.Errorf("answer %q: got %t, want %t", answer.input, got, answer.want)
		}
	}
	if confirm("Use paid transcription?") {
		t.Fatal("EOF approved a paid request")
	}
}
