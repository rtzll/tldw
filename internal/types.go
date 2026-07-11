package internal

import (
	"fmt"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

// ContentType represents the type of YouTube content
type ContentType = tldw.ContentType

const (
	ContentTypeUnknown  = tldw.ContentTypeUnknown
	ContentTypeVideo    = tldw.ContentTypeVideo
	ContentTypePlaylist = tldw.ContentTypePlaylist
	ContentTypeChannel  = tldw.ContentTypeChannel
	ContentTypeCommand  = tldw.ContentTypeCommand
)

// ParsedArg represents the result of parsing a command line argument.
// It may contain an Error; use YouTubeRef after validation in application code.
type ParsedArg struct {
	ContentType   ContentType
	OriginalInput string
	NormalizedURL string
	ID            string
	Error         error
}

// YouTubeRef is a validated YouTube content reference used after boundary parsing.
type YouTubeRef = tldw.YouTubeRef

func newYouTubeRef(parsed *ParsedArg) YouTubeRef {
	return YouTubeRef{
		ContentType:   parsed.ContentType,
		OriginalInput: parsed.OriginalInput,
		NormalizedURL: parsed.NormalizedURL,
		ID:            parsed.ID,
	}
}

// Ref returns the validated reference represented by this parse result.
func (p *ParsedArg) Ref() (YouTubeRef, error) {
	if p == nil || !p.IsValid() {
		if p != nil && p.Error != nil {
			return YouTubeRef{}, p.Error
		}
		return YouTubeRef{}, fmt.Errorf("input is not valid YouTube content")
	}
	return newYouTubeRef(p), nil
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
