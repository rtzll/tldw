package internal

import (
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

// ParseYouTubeArg parses and validates a supported YouTube content reference.
func ParseYouTubeArg(arg string) (YouTubeRef, error) {
	return tldw.ParseReference(arg)
}

// ParseVideoArg parses and validates a YouTube video reference.
func ParseVideoArg(arg string) (YouTubeRef, error) {
	return tldw.ParseVideoRef(arg)
}

func IsValidYouTubeID(id string) bool { return tldw.IsValidVideoID(id) }

func IsLikelyCommand(arg string) bool {
	arg = strings.TrimSpace(arg)
	return len(arg) <= 10 && !IsValidYouTubeID(arg) && !IsValidPlaylistID(arg)
}

func IsValidPlaylistID(id string) bool { return tldw.IsValidPlaylistID(id) }
