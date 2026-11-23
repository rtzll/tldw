package cmd

import (
	"errors"
	"io"
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
	confirm := confirmationPrompt(t.Context(), strings.NewReader(input.String()), io.Discard, true)
	for _, answer := range answers {
		if got, err := confirm("Use paid transcription?"); err != nil || got != answer.want {
			t.Errorf("answer %q: got %t, want %t", answer.input, got, answer.want)
		}
	}
	if got, err := confirm("Use paid transcription?"); got || err != nil {
		t.Fatal("EOF approved a paid request")
	}
}

func TestNoninteractiveConsentConsumesNoInput(t *testing.T) {
	in := strings.NewReader("yes")
	var out strings.Builder
	ok, err := confirmationPrompt(t.Context(), in, &out, false)("Spend?")
	if ok || !errors.Is(err, errConsentRequired) || in.Len() != 3 || out.Len() != 0 {
		t.Fatalf("ok=%v err=%v input=%d output=%q", ok, err, in.Len(), out.String())
	}
}
