package tldw

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type ContentType int

const (
	ContentTypeUnknown ContentType = iota
	ContentTypeVideo
	ContentTypePlaylist
	ContentTypeChannel
	ContentTypeCommand
)

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

// YouTubeRef is a validated YouTube content reference.
type YouTubeRef struct {
	ContentType   ContentType
	OriginalInput string
	NormalizedURL string
	ID            string
}

type PlaylistInfo struct {
	Title  string
	Videos []YouTubeRef
}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func validVideoRef(ref YouTubeRef) bool {
	return ref.ContentType == ContentTypeVideo && IsValidVideoID(ref.ID)
}

func IsValidVideoID(id string) bool { return videoIDPattern.MatchString(id) }

// ParseVideoRef validates a video ID or canonical YouTube video URL.
func ParseVideoRef(input string) (YouTubeRef, error) {
	original := strings.TrimSpace(input)
	id := original
	if !IsValidVideoID(id) {
		parsed, err := url.Parse(original)
		if err != nil {
			return YouTubeRef{}, fmt.Errorf("parsing URL: %w", err)
		}
		if parsed.Host != "youtube.com" && parsed.Host != "www.youtube.com" && parsed.Host != "youtu.be" {
			return YouTubeRef{}, fmt.Errorf("not a YouTube URL")
		}
		id = parsed.Query().Get("v")
		if parsed.Host == "youtu.be" {
			id = strings.TrimPrefix(parsed.Path, "/")
		}
	}
	if !IsValidVideoID(id) {
		return YouTubeRef{}, fmt.Errorf("expected a valid YouTube video")
	}
	return YouTubeRef{
		ContentType:   ContentTypeVideo,
		OriginalInput: original,
		NormalizedURL: "https://www.youtube.com/watch?v=" + id,
		ID:            id,
	}, nil
}
