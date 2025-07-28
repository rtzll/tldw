package internal

import (
	"fmt"
	"strings"
)

// ContentType represents the type of YouTube content
type ContentType int

const (
	ContentTypeUnknown ContentType = iota
	ContentTypeVideo
	ContentTypePlaylist
	ContentTypeChannel
	ContentTypeCommand
)

// String returns a human-readable representation of the content type
func (ct ContentType) String() string {
	switch ct {
	case ContentTypeVideo:
		return "video"
	case ContentTypePlaylist:
		return "playlist"
	case ContentTypeChannel:
		return "channel"
	case ContentTypeCommand:
		return "command"
	default:
		return "unknown"
	}
}

// ParsedArg represents the result of parsing a command line argument
type ParsedArg struct {
	ContentType   ContentType
	OriginalInput string
	NormalizedURL string
	ID            string
	Error         error
}

// IsValid returns true if the parsed argument is valid and has no errors
func (p *ParsedArg) IsValid() bool {
	return p.Error == nil && p.ContentType != ContentTypeUnknown && p.ContentType != ContentTypeCommand
}

// String returns a formatted representation of the parsed argument
func (p *ParsedArg) String() string {
	if p.Error != nil {
		return fmt.Sprintf("ParsedArg{type=%s, input=%q, error=%v}", p.ContentType, p.OriginalInput, p.Error)
	}
	return fmt.Sprintf("ParsedArg{type=%s, id=%s, url=%s}", p.ContentType, p.ID, p.NormalizedURL)
}

// SuggestCorrection provides helpful suggestions for invalid inputs
func (p *ParsedArg) SuggestCorrection(availableCommands []string) string {
	if p.ContentType != ContentTypeCommand {
		return ""
	}

	input := strings.ToLower(p.OriginalInput)
	var suggestions []string

	// Find similar commands
	for _, cmd := range availableCommands {
		if strings.Contains(cmd, input) || strings.Contains(input, cmd) {
			suggestions = append(suggestions, cmd)
		}
	}

	if len(suggestions) > 0 {
		return fmt.Sprintf("did you mean: %s", strings.Join(suggestions, ", "))
	}

	return "use --help to see available commands"
}
