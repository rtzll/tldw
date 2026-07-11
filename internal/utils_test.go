package internal

import "testing"

func TestParseYouTubeArg(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKind ContentType
		wantID   string
		wantURL  string
		wantErr  bool
	}{
		{name: "video ID", input: "dQw4w9WgXcQ", wantKind: ContentTypeVideo, wantID: "dQw4w9WgXcQ", wantURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{name: "watch URL", input: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", wantKind: ContentTypeVideo, wantID: "dQw4w9WgXcQ", wantURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{name: "short URL with playlist parameter", input: "https://youtu.be/dQw4w9WgXcQ?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", wantKind: ContentTypeVideo, wantID: "dQw4w9WgXcQ", wantURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{name: "playlist ID", input: "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", wantKind: ContentTypePlaylist, wantID: "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", wantURL: "https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"},
		{name: "UU playlist URL", input: "https://www.youtube.com/playlist?list=UUx5XG1OV2P6uZZ5FS", wantKind: ContentTypePlaylist, wantID: "UUx5XG1OV2P6uZZ5FS", wantURL: "https://www.youtube.com/playlist?list=UUx5XG1OV2P6uZZ5FS"},
		{name: "channel ID URL", input: "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw", wantKind: ContentTypeChannel, wantID: "UC_x5XG1OV2P6uZZ5FSM9Ttw", wantURL: "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw"},
		{name: "channel handle", input: "@mkbhd", wantKind: ContentTypeChannel, wantID: "@mkbhd", wantURL: "https://www.youtube.com/@mkbhd"},
		{name: "custom channel URL", input: "https://www.youtube.com/c/SomeChannel", wantKind: ContentTypeChannel, wantID: "SomeChannel", wantURL: "https://www.youtube.com/c/SomeChannel"},
		{name: "user channel URL", input: "https://www.youtube.com/user/SomeUser", wantKind: ContentTypeChannel, wantID: "SomeUser", wantURL: "https://www.youtube.com/user/SomeUser"},
		{name: "likely channel handle", input: "randomtext123", wantKind: ContentTypeChannel, wantID: "@randomtext123", wantURL: "https://www.youtube.com/@randomtext123"},
		{name: "command", input: "help", wantErr: true},
		{name: "non YouTube URL", input: "https://example.com/watch?v=dQw4w9WgXcQ", wantErr: true},
		{name: "unsupported path", input: "https://www.youtube.com/shorts/dQw4w9WgXcQ", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, err := ParseYouTubeArg(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseYouTubeArg(%q) succeeded", test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseYouTubeArg(%q) error = %v", test.input, err)
			}
			if ref.Kind() != test.wantKind || ref.ID() != test.wantID || ref.URL() != test.wantURL {
				t.Fatalf("ParseYouTubeArg(%q) = kind %s, ID %q, URL %q", test.input, ref.Kind(), ref.ID(), ref.URL())
			}
		})
	}
}

func TestParseVideoArgRejectsNonVideoInput(t *testing.T) {
	for _, input := range []string{"PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", "../outside", "https://youtu.be/short"} {
		if _, err := ParseVideoArg(input); err == nil {
			t.Fatalf("ParseVideoArg(%q) succeeded", input)
		}
	}
}

func TestYouTubeIDValidation(t *testing.T) {
	if !IsValidYouTubeID("dQw4w9WgXcQ") || IsValidYouTubeID("too-short") {
		t.Fatal("video ID validation returned an unexpected result")
	}
	if !IsValidPlaylistID("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq") || !IsValidPlaylistID("UUx5XG1OV2P6uZZ5FS") || IsValidPlaylistID("PL123") {
		t.Fatal("playlist ID validation returned an unexpected result")
	}
}

func TestIsLikelyCommand(t *testing.T) {
	if !IsLikelyCommand("help") {
		t.Fatal("IsLikelyCommand(help) = false")
	}
	if IsLikelyCommand("dQw4w9WgXcQ") || IsLikelyCommand("thisisjustarandomstring") {
		t.Fatal("IsLikelyCommand() classified content as a command")
	}
}

func TestValidateModel(t *testing.T) {
	for _, model := range []string{"gpt-5.4-mini", "gpt-4o"} {
		if err := ValidateModel(model); err != nil {
			t.Fatalf("ValidateModel(%q) error = %v", model, err)
		}
	}
	for _, model := range []string{"", "GPT-4o", "gpt-3.5-turbo"} {
		if err := ValidateModel(model); err == nil {
			t.Fatalf("ValidateModel(%q) succeeded", model)
		}
	}
}

func TestValidateOpenAIAPIKey(t *testing.T) {
	if err := ValidateOpenAIAPIKey("sk-test123"); err != nil {
		t.Fatalf("ValidateOpenAIAPIKey() error = %v", err)
	}
	if err := ValidateOpenAIAPIKey(""); err == nil {
		t.Fatal("ValidateOpenAIAPIKey() accepted an empty key")
	}
}
