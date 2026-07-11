package internal

import "testing"

func TestContentTypeString(t *testing.T) {
	tests := map[ContentType]string{
		ContentTypeUnknown:  "unknown",
		ContentTypeVideo:    "video",
		ContentTypePlaylist: "playlist",
		ContentTypeChannel:  "channel",
	}
	for contentType, want := range tests {
		if got := contentType.String(); got != want {
			t.Errorf("ContentType.String() = %q, want %q", got, want)
		}
	}
}

func TestSuggestCommand(t *testing.T) {
	commands := []string{"help", "version"}
	if got := SuggestCommand("ver", commands); got != "did you mean: version" {
		t.Fatalf("SuggestCommand() = %q", got)
	}
	if got := SuggestCommand("xyz", commands); got != "use --help to see available commands" {
		t.Fatalf("SuggestCommand() = %q", got)
	}
}
