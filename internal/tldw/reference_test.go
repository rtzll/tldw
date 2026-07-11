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

func TestParseReferenceNormalizesSupportedContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKind tldw.ContentType
		wantID   string
		wantURL  string
		wantErr  bool
	}{
		{name: "video ID", input: "dQw4w9WgXcQ", wantKind: tldw.ContentTypeVideo, wantID: "dQw4w9WgXcQ", wantURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{name: "watch URL", input: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", wantKind: tldw.ContentTypeVideo, wantID: "dQw4w9WgXcQ", wantURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{name: "short URL with playlist", input: "https://youtu.be/dQw4w9WgXcQ?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", wantKind: tldw.ContentTypeVideo, wantID: "dQw4w9WgXcQ", wantURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{name: "playlist ID", input: "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", wantKind: tldw.ContentTypePlaylist, wantID: "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", wantURL: "https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"},
		{name: "channel URL", input: "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw", wantKind: tldw.ContentTypeChannel, wantID: "UC_x5XG1OV2P6uZZ5FSM9Ttw", wantURL: "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw"},
		{name: "channel handle", input: "@mkbhd", wantKind: tldw.ContentTypeChannel, wantID: "@mkbhd", wantURL: "https://www.youtube.com/@mkbhd"},
		{name: "custom channel", input: "https://www.youtube.com/c/SomeChannel", wantKind: tldw.ContentTypeChannel, wantID: "SomeChannel", wantURL: "https://www.youtube.com/c/SomeChannel"},
		{name: "legacy user channel", input: "https://www.youtube.com/user/SomeUser", wantKind: tldw.ContentTypeChannel, wantID: "SomeUser", wantURL: "https://www.youtube.com/user/SomeUser"},
		{name: "not YouTube", input: "https://example.com/watch?v=dQw4w9WgXcQ", wantErr: true},
		{name: "unsupported path", input: "https://www.youtube.com/shorts/dQw4w9WgXcQ", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, err := tldw.ParseReference(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseReference(%q) succeeded", test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseReference(%q) error = %v", test.input, err)
			}
			if ref.Kind() != test.wantKind || ref.ID() != test.wantID || ref.URL() != test.wantURL {
				t.Fatalf("ParseReference(%q) = kind %s, ID %q, URL %q", test.input, ref.Kind(), ref.ID(), ref.URL())
			}
		})
	}
}

func TestReferenceIDValidation(t *testing.T) {
	if !tldw.IsValidVideoID("dQw4w9WgXcQ") || tldw.IsValidVideoID("too-short") {
		t.Fatal("video ID validation returned an unexpected result")
	}
	if !tldw.IsValidPlaylistID("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq") || !tldw.IsValidPlaylistID("UUx5XG1OV2P6uZZ5FS") || tldw.IsValidPlaylistID("PL123") {
		t.Fatal("playlist ID validation returned an unexpected result")
	}
}
