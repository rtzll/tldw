package tldw

import "regexp"

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
	return ref.ContentType == ContentTypeVideo && videoIDPattern.MatchString(ref.ID)
}
