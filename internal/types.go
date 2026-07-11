package internal

import (
	"fmt"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

type ContentType = tldw.ContentType

const (
	ContentTypeUnknown  = tldw.ContentTypeUnknown
	ContentTypeVideo    = tldw.ContentTypeVideo
	ContentTypePlaylist = tldw.ContentTypePlaylist
	ContentTypeChannel  = tldw.ContentTypeChannel
)

type YouTubeRef = tldw.YouTubeRef

// SuggestCommand returns CLI guidance for an input that resembles a command.
func SuggestCommand(input string, availableCommands []string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var suggestions []string
	for _, command := range availableCommands {
		if strings.Contains(command, input) || strings.Contains(input, command) {
			suggestions = append(suggestions, command)
		}
	}
	if len(suggestions) > 0 {
		return fmt.Sprintf("did you mean: %s", strings.Join(suggestions, ", "))
	}
	return "use --help to see available commands"
}
