package tldw_test

import (
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func FuzzParseReference(f *testing.F) {
	for _, seed := range []string{
		"dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq",
		"PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq",
		"@mkbhd",
		"../outside",
		"\x00https://youtube.com/watch?v=dQw4w9WgXcQ",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		ref, err := tldw.ParseReference(input)
		if err != nil {
			return
		}
		if ref.Kind() == tldw.ContentTypeUnknown || ref.ID() == "" || ref.URL() == "" {
			t.Fatalf("ParseReference(%q) returned an incomplete reference", input)
		}
		roundTripped, err := tldw.ParseReference(ref.URL())
		if err != nil {
			t.Fatalf("ParseReference(%q) returned URL %q that cannot be parsed: %v", input, ref.URL(), err)
		}
		if roundTripped.Kind() != ref.Kind() || roundTripped.ID() != ref.ID() {
			t.Fatalf("reference changed after URL round trip: %#v -> %#v", ref, roundTripped)
		}
	})
}
