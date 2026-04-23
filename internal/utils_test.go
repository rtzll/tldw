package internal

import (
	"testing"
)

func TestDetectVideoID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid video ID", "dQw4w9WgXcQ", true},
		{"valid with underscore", "a_b-c123DEF", true},
		{"too short", "dQw4w9WgXc", false},
		{"too long", "dQw4w9WgXcQQ", false},
		{"invalid chars", "dQw4w9WgXc!", false},
		{"empty", "", false},
		{"URL not ID", "https://youtube.com/watch?v=dQw4w9WgXcQ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectVideoID(tt.id); got != tt.want {
				t.Errorf("detectVideoID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestDetectPlaylistID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid PL 18 chars", "PLSE8ODhjZXjYDBpQn", true},
		{"valid PL 34 chars", "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", true},
		{"invalid prefix", "OLSE8ODhjZXjYDBpQnS", false},
		{"too short", "PL123", false},
		{"empty", "", false},
		{"video ID", "dQw4w9WgXcQ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectPlaylistID(tt.id); got != tt.want {
				t.Errorf("detectPlaylistID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestDetectChannelID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid channel ID", "UC_x5XG1OV2P6uZZ5FSM9Ttw", true},
		{"too short", "UC_x5XG1OV2P6uZZ5FSM9Tt", false},
		{"wrong prefix", "UD_x5XG1OV2P6uZZ5FSM9Ttw", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectChannelID(tt.id); got != tt.want {
				t.Errorf("detectChannelID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestDetectChannelHandle(t *testing.T) {
	tests := []struct {
		name    string
		handle  string
		want    bool
	}{
		{"valid with @", "@mkbhd", true},
		{"valid without @", "mkbhd", true},
		{"valid with dot", "@Some.Channel", true},
		{"too short", "@ab", false},
		{"too long", "@abcdefghijklmnopqrstuvwxyz12345", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectChannelHandle(tt.handle); got != tt.want {
				t.Errorf("detectChannelHandle(%q) = %v, want %v", tt.handle, got, tt.want)
			}
		})
	}
}

func TestDetectCommand(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{"known command help", "help", true},
		{"known command version", "version", true},
		{"command-like install", "install", true},
		{"video ID", "dQw4w9WgXcQ", false},
		{"channel handle", "@mkbhd", false},
		{"too short", "a", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectCommand(tt.arg); got != tt.want {
				t.Errorf("detectCommand(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestContainsDigit(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"has digit", "abc123", true},
		{"no digit", "abcdef", false},
		{"empty", "", false},
		{"only digit", "123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsDigit(tt.s); got != tt.want {
				t.Errorf("containsDigit(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want ContentType
	}{
		{"video ID", "dQw4w9WgXcQ", ContentTypeVideo},
		{"playlist ID", "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", ContentTypePlaylist},
		{"channel ID", "UC_x5XG1OV2P6uZZ5FSM9Ttw", ContentTypeChannel},
		{"channel handle", "@mkbhd", ContentTypeChannel},
		{"command", "help", ContentTypeCommand},
		{"unknown", "notavalidthing", ContentTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectContentType(tt.arg); got != tt.want {
				t.Errorf("detectContentType(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestParseYouTubeURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantType    ContentType
		wantID      string
		wantError   bool
	}{
		{"watch URL", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", ContentTypeVideo, "dQw4w9WgXcQ", false},
		{"short URL", "https://youtu.be/dQw4w9WgXcQ", ContentTypeVideo, "dQw4w9WgXcQ", false},
		{"playlist URL", "https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", ContentTypePlaylist, "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", false},
		{"channel URL", "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw", ContentTypeChannel, "UC_x5XG1OV2P6uZZ5FSM9Ttw", false},
		{"handle URL", "https://www.youtube.com/@mkbhd", ContentTypeChannel, "@mkbhd", false},
		{"custom channel URL", "https://www.youtube.com/c/SomeChannel", ContentTypeChannel, "SomeChannel", false},
		{"user channel URL", "https://www.youtube.com/user/SomeUser", ContentTypeChannel, "SomeUser", false},
		{"not YouTube", "https://example.com/watch?v=dQw4w9WgXcQ", ContentTypeUnknown, "", true},
		{"invalid youtu.be", "https://youtu.be/short", ContentTypeUnknown, "", true},
		{"unsupported path", "https://www.youtube.com/shorts/dQw4w9WgXcQ", ContentTypeUnknown, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseYouTubeURL(tt.url)
			if tt.wantError {
				if got.Error == nil {
					t.Errorf("parseYouTubeURL(%q) expected error, got nil", tt.url)
				}
				return
			}
			if got.Error != nil {
				t.Errorf("parseYouTubeURL(%q) unexpected error: %v", tt.url, got.Error)
				return
			}
			if got.ContentType != tt.wantType {
				t.Errorf("parseYouTubeURL(%q) type = %v, want %v", tt.url, got.ContentType, tt.wantType)
			}
			if got.ID != tt.wantID {
				t.Errorf("parseYouTubeURL(%q) ID = %v, want %v", tt.url, got.ID, tt.wantID)
			}
		})
	}
}

func TestParseArgNew(t *testing.T) {
	tests := []struct {
		name          string
		arg           string
		wantType      ContentType
		wantID        string
		wantNormalized string
		wantError     bool
	}{
		{"video ID", "dQw4w9WgXcQ", ContentTypeVideo, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", false},
		{"watch URL", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", ContentTypeVideo, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", false},
		{"playlist ID", "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", ContentTypePlaylist, "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", "https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", false},
		{"channel handle", "@mkbhd", ContentTypeChannel, "@mkbhd", "https://www.youtube.com/@mkbhd", false},
		{"command", "help", ContentTypeCommand, "", "", true},
		{"unknown", "randomtext123", ContentTypeChannel, "@randomtext123", "https://www.youtube.com/@randomtext123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseArgNew(tt.arg)
			if tt.wantError {
				if got.Error == nil {
					t.Errorf("ParseArgNew(%q) expected error, got nil", tt.arg)
				}
				return
			}
			if got.Error != nil {
				t.Errorf("ParseArgNew(%q) unexpected error: %v", tt.arg, got.Error)
				return
			}
			if got.ContentType != tt.wantType {
				t.Errorf("ParseArgNew(%q) type = %v, want %v", tt.arg, got.ContentType, tt.wantType)
			}
			if got.ID != tt.wantID {
				t.Errorf("ParseArgNew(%q) ID = %v, want %v", tt.arg, got.ID, tt.wantID)
			}
			if got.NormalizedURL != tt.wantNormalized {
				t.Errorf("ParseArgNew(%q) URL = %v, want %v", tt.arg, got.NormalizedURL, tt.wantNormalized)
			}
		})
	}
}

func TestParseArg(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantURL string
		wantID  string
	}{
		{"video ID", "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"command returns original", "help", "help", "help"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, id := ParseArg(tt.arg)
			if url != tt.wantURL {
				t.Errorf("ParseArg(%q) URL = %v, want %v", tt.arg, url, tt.wantURL)
			}
			if id != tt.wantID {
				t.Errorf("ParseArg(%q) ID = %v, want %v", tt.arg, id, tt.wantID)
			}
		})
	}
}

func TestIsValidYouTubeID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid", "dQw4w9WgXcQ", true},
		{"too short", "dQw4w9WgXc", false},
		{"too long", "dQw4w9WgXcQQ", false},
		{"invalid chars", "dQw4w9WgXc!", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidYouTubeID(tt.id); got != tt.want {
				t.Errorf("IsValidYouTubeID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestIsValidPlaylistID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid PL", "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", true},
		{"valid UU", "UUx5XG1OV2P6uZZ5FS", true},
		{"valid FL 18", "FLx5XG1OV2P6uZZ5FS", true},
		{"valid OLAK5uy_ 40", "OLAK5uy_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"invalid short", "PL123", false},
		{"video ID", "dQw4w9WgXcQ", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPlaylistID(tt.id); got != tt.want {
				t.Errorf("IsValidPlaylistID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestIsLikelyCommand(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{"short string", "help", true},
		{"video ID", "dQw4w9WgXcQ", false},
		{"playlist ID", "PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq", false},
		{"long string", "thisisjustarandomstring", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLikelyCommand(tt.arg); got != tt.want {
				t.Errorf("IsLikelyCommand(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestValidateModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{"empty", "", false},
		{"valid gpt-5-nano", "gpt-5-nano", true},
		{"valid gpt-4o", "gpt-4o", true},
		{"invalid chars", "GPT-4o", false},
		{"unsupported", "gpt-3.5-turbo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModel(tt.model)
			if tt.want && err != nil {
				t.Errorf("ValidateModel(%q) unexpected error: %v", tt.model, err)
			}
			if !tt.want && err == nil {
				t.Errorf("ValidateModel(%q) expected error, got nil", tt.model)
			}
		})
	}
}

func TestValidateOpenAIAPIKey(t *testing.T) {
	tests := []struct {
		name   string
		apiKey string
		want   bool
	}{
		{"empty", "", false},
		{"valid", "sk-test123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOpenAIAPIKey(tt.apiKey)
			if tt.want && err != nil {
				t.Errorf("ValidateOpenAIAPIKey(%q) unexpected error: %v", tt.apiKey, err)
			}
			if !tt.want && err == nil {
				t.Errorf("ValidateOpenAIAPIKey(%q) expected error, got nil", tt.apiKey)
			}
		})
	}
}
