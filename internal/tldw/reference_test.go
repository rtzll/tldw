package tldw_test

import (
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestParseVideoRefNormalizesSupportedInputs(t *testing.T) {
	for _, input := range []string{
		"dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	} {
		ref, err := tldw.ParseVideoRef(input)
		if err != nil {
			t.Fatalf("ParseVideoRef(%q) error = %v", input, err)
		}
		if ref.ID() != "dQw4w9WgXcQ" || ref.URL() != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
			t.Fatalf("ParseVideoRef(%q) = %+v", input, ref)
		}
	}
}

func TestParseVideoRefRejectsNonVideoInput(t *testing.T) {
	for _, input := range []string{"https://example.com/video", "https://youtu.be/short", "../outside"} {
		if _, err := tldw.ParseVideoRef(input); err == nil {
			t.Fatalf("ParseVideoRef(%q) succeeded", input)
		}
	}
}
