package internal

import (
	"testing"
)

func TestContentTypeString(t *testing.T) {
	tests := []struct {
		name string
		ct   ContentType
		want string
	}{
		{"unknown", ContentTypeUnknown, "unknown"},
		{"video", ContentTypeVideo, "video"},
		{"playlist", ContentTypePlaylist, "playlist"},
		{"channel", ContentTypeChannel, "channel"},
		{"command", ContentTypeCommand, "command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ct.String(); got != tt.want {
				t.Errorf("ContentType.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParsedArgIsValid(t *testing.T) {
	tests := []struct {
		name string
		arg  *ParsedArg
		want bool
	}{
		{"valid video", &ParsedArg{ContentType: ContentTypeVideo, Error: nil}, true},
		{"valid playlist", &ParsedArg{ContentType: ContentTypePlaylist, Error: nil}, true},
		{"unknown type", &ParsedArg{ContentType: ContentTypeUnknown, Error: nil}, false},
		{"command type", &ParsedArg{ContentType: ContentTypeCommand, Error: nil}, false},
		{"with error", &ParsedArg{ContentType: ContentTypeVideo, Error: errTest}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.arg.IsValid(); got != tt.want {
				t.Errorf("ParsedArg.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

var errTest = errTestType{}

type errTestType struct{}

func (errTestType) Error() string { return "test error" }

func TestParsedArgSuggestCorrection(t *testing.T) {
	tests := []struct {
		name             string
		arg              *ParsedArg
		availableCommands []string
		want             string
	}{
		{
			name:             "not a command",
			arg:              &ParsedArg{ContentType: ContentTypeVideo, OriginalInput: "video"},
			availableCommands: []string{"help", "version"},
			want:             "",
		},
		{
			name:             "exact match",
			arg:              &ParsedArg{ContentType: ContentTypeCommand, OriginalInput: "help"},
			availableCommands: []string{"help", "version"},
			want:             "did you mean: help",
		},
		{
			name:             "partial match",
			arg:              &ParsedArg{ContentType: ContentTypeCommand, OriginalInput: "ver"},
			availableCommands: []string{"help", "version"},
			want:             "did you mean: version",
		},
		{
			name:             "no match",
			arg:              &ParsedArg{ContentType: ContentTypeCommand, OriginalInput: "xyz"},
			availableCommands: []string{"help", "version"},
			want:             "use --help to see available commands",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.arg.SuggestCorrection(tt.availableCommands); got != tt.want {
				t.Errorf("ParsedArg.SuggestCorrection() = %v, want %v", got, tt.want)
			}
		})
	}
}
