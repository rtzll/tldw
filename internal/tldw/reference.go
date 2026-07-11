package tldw

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

type ContentType int

const (
	ContentTypeUnknown ContentType = iota
	ContentTypeVideo
	ContentTypePlaylist
	ContentTypeChannel
)

func (ct ContentType) String() string {
	switch ct {
	case ContentTypeVideo:
		return "video"
	case ContentTypePlaylist:
		return "playlist"
	case ContentTypeChannel:
		return "channel"
	default:
		return "unknown"
	}
}

// YouTubeRef is a validated YouTube content reference. Values can only be
// created by the parsing functions in this package.
type YouTubeRef struct {
	kind          ContentType
	normalizedURL string
	id            string
}

func (ref YouTubeRef) Kind() ContentType { return ref.kind }
func (ref YouTubeRef) URL() string       { return ref.normalizedURL }
func (ref YouTubeRef) ID() string        { return ref.id }
func (ref YouTubeRef) IsVideo() bool     { return ref.kind == ContentTypeVideo }
func (ref YouTubeRef) IsPlaylist() bool  { return ref.kind == ContentTypePlaylist }

type PlaylistInfo struct {
	Title  string
	Videos []YouTubeRef
}

var (
	videoIDPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	playlistIDPattern    = regexp.MustCompile(`^PL[A-Za-z0-9_-]{16}$|^PL[A-Za-z0-9_-]{32}$`)
	channelIDPattern     = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	channelHandlePattern = regexp.MustCompile(`^@?[A-Za-z0-9._-]{3,30}$`)
	validIDCharacters    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

func validVideoRef(ref YouTubeRef) bool { return ref.IsVideo() && IsValidVideoID(ref.ID()) }

func IsValidVideoID(id string) bool { return videoIDPattern.MatchString(id) }

func IsValidPlaylistID(id string) bool {
	if strings.HasPrefix(id, "PL") {
		return playlistIDPattern.MatchString(id)
	}
	for _, prefix := range []string{"UU", "FL", "RD", "LP", "BP", "QL", "SV", "EL", "LL", "UC"} {
		if strings.HasPrefix(id, prefix) && (len(id) == 18 || len(id) == 34) {
			return validIDCharacters.MatchString(id)
		}
	}
	if (strings.HasPrefix(id, "OLAK5uy_") || strings.HasPrefix(id, "RDCLAK5uy_")) && len(id) == 40 {
		return validIDCharacters.MatchString(id)
	}
	return false
}

// ParseReference validates and normalizes supported YouTube IDs, handles, and URLs.
func ParseReference(input string) (YouTubeRef, error) {
	original := strings.TrimSpace(input)
	if original == "" {
		return YouTubeRef{}, fmt.Errorf("YouTube reference is empty")
	}
	if strings.HasPrefix(original, "http://") || strings.HasPrefix(original, "https://") {
		return parseReferenceURL(original)
	}
	if IsValidVideoID(original) {
		return videoRef(original), nil
	}
	if IsValidPlaylistID(original) {
		return playlistRef(original), nil
	}
	if channelIDPattern.MatchString(original) {
		return channelRef(original, "https://www.youtube.com/channel/"+original), nil
	}
	if isLikelyChannelHandle(original) {
		handle := original
		if !strings.HasPrefix(handle, "@") {
			handle = "@" + handle
		}
		return channelRef(handle, "https://www.youtube.com/"+handle), nil
	}
	return YouTubeRef{}, fmt.Errorf("unable to determine YouTube content type for %q", original)
}

// ParseVideoRef validates a video ID or supported YouTube video URL.
func ParseVideoRef(input string) (YouTubeRef, error) {
	ref, err := ParseReference(input)
	if err != nil {
		return YouTubeRef{}, err
	}
	if !ref.IsVideo() {
		return YouTubeRef{}, fmt.Errorf("expected a YouTube video, got %s", ref.Kind())
	}
	return ref, nil
}

func parseReferenceURL(original string) (YouTubeRef, error) {
	parsed, err := url.Parse(original)
	if err != nil {
		return YouTubeRef{}, fmt.Errorf("parsing YouTube URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "youtube.com" && host != "www.youtube.com" && host != "youtu.be" {
		return YouTubeRef{}, fmt.Errorf("not a YouTube URL: %s", host)
	}
	if host == "youtu.be" {
		id := strings.Trim(parsed.Path, "/")
		if !IsValidVideoID(id) {
			return YouTubeRef{}, fmt.Errorf("invalid video ID in youtu.be URL: %s", id)
		}
		return videoRef(id), nil
	}

	switch {
	case parsed.Path == "/watch":
		if id := parsed.Query().Get("v"); IsValidVideoID(id) {
			return videoRef(id), nil
		}
		if id := parsed.Query().Get("list"); IsValidPlaylistID(id) {
			return playlistRef(id), nil
		}
		return YouTubeRef{}, fmt.Errorf("no valid video or playlist ID found in watch URL")
	case parsed.Path == "/playlist":
		id := parsed.Query().Get("list")
		if !IsValidPlaylistID(id) {
			return YouTubeRef{}, fmt.Errorf("invalid playlist ID: %s", id)
		}
		return playlistRef(id), nil
	case strings.HasPrefix(parsed.Path, "/channel/"):
		id := strings.TrimPrefix(parsed.Path, "/channel/")
		if !channelIDPattern.MatchString(id) {
			return YouTubeRef{}, fmt.Errorf("invalid channel ID: %s", id)
		}
		return channelRef(id, "https://www.youtube.com/channel/"+id), nil
	case strings.HasPrefix(parsed.Path, "/@"):
		handle := strings.TrimPrefix(parsed.Path, "/")
		if !channelHandlePattern.MatchString(handle) {
			return YouTubeRef{}, fmt.Errorf("invalid channel handle: %s", handle)
		}
		return channelRef(handle, "https://www.youtube.com/"+handle), nil
	case strings.HasPrefix(parsed.Path, "/c/"):
		return parseNamedChannel(strings.TrimPrefix(parsed.Path, "/c/"), "/c/")
	case strings.HasPrefix(parsed.Path, "/user/"):
		return parseNamedChannel(strings.TrimPrefix(parsed.Path, "/user/"), "/user/")
	default:
		return YouTubeRef{}, fmt.Errorf("unsupported YouTube URL path: %s", parsed.Path)
	}
}

func parseNamedChannel(name, prefix string) (YouTubeRef, error) {
	if len(name) < 3 || len(name) > 30 || !validIDCharacters.MatchString(name) {
		return YouTubeRef{}, fmt.Errorf("invalid channel name: %s", name)
	}
	return channelRef(name, "https://www.youtube.com"+prefix+name), nil
}

func videoRef(id string) YouTubeRef {
	return YouTubeRef{kind: ContentTypeVideo, normalizedURL: "https://www.youtube.com/watch?v=" + id, id: id}
}

func playlistRef(id string) YouTubeRef {
	return YouTubeRef{kind: ContentTypePlaylist, normalizedURL: "https://www.youtube.com/playlist?list=" + id, id: id}
}

func channelRef(id, normalized string) YouTubeRef {
	return YouTubeRef{kind: ContentTypeChannel, normalizedURL: normalized, id: id}
}

func isLikelyChannelHandle(input string) bool {
	if !channelHandlePattern.MatchString(input) {
		return false
	}
	handle := strings.TrimPrefix(input, "@")
	if slices.Contains([]string{"help", "version", "config", "settings", "options", "default", "example", "test", "demo", "sample", "invalid", "error", "command", "input", "output", "file", "directory", "path", "user", "admin", "system", "server", "client", "local"}, strings.ToLower(handle)) {
		return false
	}
	if !strings.ContainsAny(handle, "0123456789") && len(handle) > 10 {
		return false
	}
	return true
}
