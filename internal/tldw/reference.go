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

// ContentRef is a validated YouTube reference or X post. Values can only be
// created by the parsing functions in this package.
type ContentRef struct {
	kind          ContentType
	normalizedURL string
	id            string
	isX           bool
}

func (ref ContentRef) Kind() ContentType { return ref.kind }
func (ref ContentRef) URL() string       { return ref.normalizedURL }
func (ref ContentRef) ID() string        { return ref.id }
func (ref ContentRef) IsVideo() bool     { return ref.kind == ContentTypeVideo }
func (ref ContentRef) IsPlaylist() bool  { return ref.kind == ContentTypePlaylist }
func (ref ContentRef) IsX() bool         { return ref.isX }

// CacheKey preserves existing YouTube keys and namespaces X videos by post ID.
func (ref ContentRef) CacheKey() string {
	if ref.IsX() {
		return "x." + ref.id
	}
	return ref.id
}

type PlaylistInfo struct {
	Title  string
	Videos []ContentRef
}

const xPostID = `[1-9][0-9]{0,19}`

var (
	videoIDPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	xStatusPath          = regexp.MustCompile(`^/(?:[A-Za-z0-9_]{1,15}|i/web)/status/(` + xPostID + `)$`)
	xCacheKeyPattern     = regexp.MustCompile(`^x\.` + xPostID + `$`)
	playlistIDPattern    = regexp.MustCompile(`^PL[A-Za-z0-9_-]{16}$|^PL[A-Za-z0-9_-]{32}$`)
	channelIDPattern     = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	channelHandlePattern = regexp.MustCompile(`^@?[A-Za-z0-9._-]{3,30}$`)
	validIDCharacters    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

func validVideoRef(ref ContentRef) bool { return ref.IsVideo() && IsValidVideoCacheKey(ref.CacheKey()) }

func IsValidVideoCacheKey(key string) bool {
	return IsValidVideoID(key) || xCacheKeyPattern.MatchString(key)
}

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

// ParseReference validates and normalizes YouTube references and X post URLs.
func ParseReference(input string) (ContentRef, error) {
	original := strings.TrimSpace(input)
	if original == "" {
		return ContentRef{}, fmt.Errorf("content reference is empty")
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
	return ContentRef{}, fmt.Errorf("unable to determine YouTube content type for %q", original)
}

// ParseVideoRef validates a YouTube video ID, YouTube video URL, or X post URL.
func ParseVideoRef(input string) (ContentRef, error) {
	ref, err := ParseReference(input)
	if err != nil {
		return ContentRef{}, err
	}
	if !ref.IsVideo() {
		return ContentRef{}, fmt.Errorf("expected a YouTube video, got %s", ref.Kind())
	}
	return ref, nil
}

func parseReferenceURL(original string) (ContentRef, error) {
	parsed, err := url.Parse(original)
	if err != nil {
		return ContentRef{}, fmt.Errorf("parsing URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
		path := strings.TrimSuffix(parsed.EscapedPath(), "/")
		match := xStatusPath.FindStringSubmatch(path)
		if match == nil {
			return ContentRef{}, fmt.Errorf("expected an X post URL with a numeric status ID")
		}
		return ContentRef{kind: ContentTypeVideo, normalizedURL: "https://x.com" + path, id: match[1], isX: true}, nil
	case "youtube.com", "www.youtube.com", "youtu.be":
	default:
		return ContentRef{}, fmt.Errorf("unsupported host %q: expected a YouTube or X URL", host)
	}
	if host == "youtu.be" {
		id := strings.Trim(parsed.Path, "/")
		if !IsValidVideoID(id) {
			return ContentRef{}, fmt.Errorf("invalid video ID in youtu.be URL: %s", id)
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
		return ContentRef{}, fmt.Errorf("no valid video or playlist ID found in watch URL")
	case parsed.Path == "/playlist":
		id := parsed.Query().Get("list")
		if !IsValidPlaylistID(id) {
			return ContentRef{}, fmt.Errorf("invalid playlist ID: %s", id)
		}
		return playlistRef(id), nil
	case strings.HasPrefix(parsed.Path, "/shorts/"):
		id := strings.TrimPrefix(parsed.Path, "/shorts/")
		if !IsValidVideoID(id) {
			return ContentRef{}, fmt.Errorf("invalid video ID in Shorts URL: %s", id)
		}
		return videoRef(id), nil
	case strings.HasPrefix(parsed.Path, "/channel/"):
		id := strings.TrimPrefix(parsed.Path, "/channel/")
		if !channelIDPattern.MatchString(id) {
			return ContentRef{}, fmt.Errorf("invalid channel ID: %s", id)
		}
		return channelRef(id, "https://www.youtube.com/channel/"+id), nil
	case strings.HasPrefix(parsed.Path, "/@"):
		handle := strings.TrimPrefix(parsed.Path, "/")
		if !channelHandlePattern.MatchString(handle) {
			return ContentRef{}, fmt.Errorf("invalid channel handle: %s", handle)
		}
		return channelRef(handle, "https://www.youtube.com/"+handle), nil
	case strings.HasPrefix(parsed.Path, "/c/"):
		return parseNamedChannel(strings.TrimPrefix(parsed.Path, "/c/"), "/c/")
	case strings.HasPrefix(parsed.Path, "/user/"):
		return parseNamedChannel(strings.TrimPrefix(parsed.Path, "/user/"), "/user/")
	default:
		return ContentRef{}, fmt.Errorf("unsupported YouTube URL path: %s", parsed.Path)
	}
}

func parseNamedChannel(name, prefix string) (ContentRef, error) {
	if len(name) < 3 || len(name) > 30 || !validIDCharacters.MatchString(name) {
		return ContentRef{}, fmt.Errorf("invalid channel name: %s", name)
	}
	return channelRef(name, "https://www.youtube.com"+prefix+name), nil
}

func videoRef(id string) ContentRef {
	return ContentRef{kind: ContentTypeVideo, normalizedURL: "https://www.youtube.com/watch?v=" + id, id: id}
}

func playlistRef(id string) ContentRef {
	return ContentRef{kind: ContentTypePlaylist, normalizedURL: "https://www.youtube.com/playlist?list=" + id, id: id}
}

func channelRef(id, normalized string) ContentRef {
	return ContentRef{kind: ContentTypeChannel, normalizedURL: normalized, id: id}
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
