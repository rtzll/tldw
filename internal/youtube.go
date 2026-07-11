package internal

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

// Content type detection patterns
var (
	// Video ID pattern: 11 characters, alphanumeric with - and _
	videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

	// Playlist ID pattern - only regular playlists (PL)
	// PL + 16 chars (18 total) or PL + 32 chars (34 total)
	playlistIDPattern = regexp.MustCompile(`^PL[A-Za-z0-9_-]{16}$|^PL[A-Za-z0-9_-]{32}$`)

	// Channel ID pattern: UC followed by 22 characters
	channelIDPattern = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)

	// Channel handle pattern: alphanumeric with dots, underscores, hyphens (3-30 chars)
	channelHandlePattern = regexp.MustCompile(`^@?[A-Za-z0-9._-]{3,30}$`)

	// Command pattern: short strings that might be commands
	commandPattern = regexp.MustCompile(`^[a-z]{2,15}$`)

	// Model name pattern: allow lowercase letters, digits, dots, underscores, hyphens
	// Examples: gpt-4o, gpt-4.1-nano, gpt-5, gpt-5.4-mini, gpt-5-mini, gpt-5-chat-latest
	modelNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,}$`)
)

// Content type detection functions

// detectVideoID checks if a string looks like a YouTube video ID
func detectVideoID(s string) bool {
	return videoIDPattern.MatchString(s)
}

// detectPlaylistID checks if a string looks like a YouTube playlist ID
func detectPlaylistID(s string) bool {
	return isValidPlaylistID(s)
}

// detectChannelID checks if a string looks like a YouTube channel ID
func detectChannelID(s string) bool {
	return channelIDPattern.MatchString(s)
}

// detectChannelHandle checks if a string looks like a YouTube channel handle
func detectChannelHandle(s string) bool {
	if !channelHandlePattern.MatchString(s) {
		return false
	}

	// Remove @ if present for length check
	handle := strings.TrimPrefix(s, "@")

	// Must be between 3-30 characters
	return len(handle) >= 3 && len(handle) <= 30
}

// detectCommand checks if a string looks like a command
func detectCommand(s string) bool {
	// Must be lowercase, short, and match known command patterns
	if !commandPattern.MatchString(s) {
		return false
	}

	// Check against known commands and common command patterns
	knownCommands := []string{
		"help", "version", "transcribe", "cp", "metadata", "mcp",
		"config", "paths", "init", "list", "show", "get", "set",
		"run", "start", "stop", "status", "info", "debug",
	}

	for _, cmd := range knownCommands {
		if cmd == s || strings.Contains(cmd, s) || strings.Contains(s, cmd) {
			return true
		}
	}

	// Additional heuristics: words that sound like commands
	commandLikeWords := []string{
		"install", "update", "remove", "delete", "create", "add",
		"edit", "modify", "change", "reset", "clear", "clean",
	}

	for _, word := range commandLikeWords {
		if strings.Contains(word, s) || strings.Contains(s, word) {
			return true
		}
	}

	return false
}

// isLikelyYouTubeChannelHandle checks if a string looks like a real YouTube channel handle
func isLikelyYouTubeChannelHandle(s string) bool {
	if !detectChannelHandle(s) {
		return false
	}

	// Remove @ if present
	handle := strings.TrimPrefix(s, "@")

	// Reject things that look more like commands or common words
	if detectCommand(handle) {
		return false
	}

	// Reject common English words that are unlikely to be channel handles
	commonWords := []string{
		"help", "version", "config", "settings", "options", "default",
		"example", "test", "demo", "sample", "invalid", "error",
		"command", "input", "output", "file", "directory", "path",
		"user", "admin", "system", "server", "client", "local",
	}

	if slices.Contains(commonWords, strings.ToLower(handle)) {
		return false
	}

	// Apply stricter rules for longer handles without numbers
	if !containsDigit(handle) {
		// For handles without numbers, they should either be:
		// 1. Very short (likely brand names like "mkbhd")
		// 2. Have mixed case or special chars (like brand names)
		// 3. Not look like common English words
		if len(handle) > 10 {
			return false // Long handles without numbers are suspicious
		}

		// If it looks like a common English word pattern, reject it
		if isCommonWordPattern(handle) {
			return false
		}
	}

	return true
}

// containsDigit checks if a string contains at least one digit
func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// isCommonWordPattern checks if a string looks like a common English word pattern
func isCommonWordPattern(s string) bool {
	s = strings.ToLower(s)

	// Common word patterns that are unlikely to be YouTube handles
	if strings.HasSuffix(s, "command") || strings.HasSuffix(s, "invalid") ||
		strings.HasSuffix(s, "error") || strings.HasSuffix(s, "test") ||
		strings.HasPrefix(s, "invalid") || strings.HasPrefix(s, "error") ||
		strings.HasPrefix(s, "test") || strings.HasPrefix(s, "example") {
		return true
	}

	// Check for common word combinations
	commonPatterns := []string{
		"invalidcommand", "testcommand", "errorcommand", "defaultvalue",
		"exampletext", "sampledata", "placeholder", "randomtext",
	}

	return slices.Contains(commonPatterns, s)
}

// detectContentType determines the most likely content type for a string
func detectContentType(s string) ContentType {
	s = strings.TrimSpace(s)

	// Check in order of specificity - most specific first
	if detectVideoID(s) {
		return ContentTypeVideo
	}

	if detectChannelID(s) {
		return ContentTypeChannel
	}

	if detectPlaylistID(s) {
		return ContentTypePlaylist
	}

	// Check for commands before channel handles to avoid false positives
	if detectCommand(s) {
		return ContentTypeCommand
	}

	if isLikelyYouTubeChannelHandle(s) {
		return ContentTypeChannel
	}

	return ContentTypeUnknown
}

// URL parsing functions

// parseYouTubeURL extracts content from various YouTube URL formats
func parseYouTubeURL(rawURL string) *ParsedArg {
	u, err := url.Parse(rawURL)
	if err != nil {
		return &ParsedArg{
			ContentType:   ContentTypeUnknown,
			OriginalInput: rawURL,
			Error:         fmt.Errorf("invalid URL format: %w", err),
		}
	}

	// Normalize host
	host := strings.ToLower(u.Host)
	if host != "www.youtube.com" && host != "youtube.com" && host != "youtu.be" {
		return &ParsedArg{
			ContentType:   ContentTypeUnknown,
			OriginalInput: rawURL,
			Error:         fmt.Errorf("not a YouTube URL: %s", host),
		}
	}

	// Handle youtu.be short URLs
	if host == "youtu.be" {
		videoID := strings.TrimPrefix(u.Path, "/")
		if detectVideoID(videoID) {
			return &ParsedArg{
				ContentType:   ContentTypeVideo,
				OriginalInput: rawURL,
				NormalizedURL: fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID),
				ID:            videoID,
			}
		}
		return &ParsedArg{
			ContentType:   ContentTypeUnknown,
			OriginalInput: rawURL,
			Error:         fmt.Errorf("invalid video ID in youtu.be URL: %s", videoID),
		}
	}

	// Handle youtube.com URLs
	switch {
	case strings.HasPrefix(u.Path, "/watch"):
		return parseWatchURL(u, rawURL)
	case strings.HasPrefix(u.Path, "/playlist"):
		return parsePlaylistURL(u, rawURL)
	case strings.HasPrefix(u.Path, "/channel/"):
		return parseChannelURL(u, rawURL)
	case strings.HasPrefix(u.Path, "/@"):
		return parseHandleURL(u, rawURL)
	case strings.HasPrefix(u.Path, "/c/"):
		return parseCustomChannelURL(u, rawURL)
	case strings.HasPrefix(u.Path, "/user/"):
		return parseUserChannelURL(u, rawURL)
	default:
		return &ParsedArg{
			ContentType:   ContentTypeUnknown,
			OriginalInput: rawURL,
			Error:         fmt.Errorf("unsupported YouTube URL path: %s", u.Path),
		}
	}
}

// parseWatchURL handles /watch URLs (videos, may also contain playlist)
func parseWatchURL(u *url.URL, originalURL string) *ParsedArg {
	videoID := u.Query().Get("v")
	playlistID := u.Query().Get("list")

	// Prioritize video over playlist if both are present
	if videoID != "" && detectVideoID(videoID) {
		return &ParsedArg{
			ContentType:   ContentTypeVideo,
			OriginalInput: originalURL,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID),
			ID:            videoID,
		}
	}

	// Check for playlist if no valid video ID
	if playlistID != "" && detectPlaylistID(playlistID) {
		return &ParsedArg{
			ContentType:   ContentTypePlaylist,
			OriginalInput: originalURL,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/playlist?list=%s", playlistID),
			ID:            playlistID,
		}
	}

	return &ParsedArg{
		ContentType:   ContentTypeUnknown,
		OriginalInput: originalURL,
		Error:         fmt.Errorf("no valid video or playlist ID found in watch URL"),
	}
}

// parsePlaylistURL handles /playlist URLs
func parsePlaylistURL(u *url.URL, originalURL string) *ParsedArg {
	playlistID := u.Query().Get("list")
	if playlistID != "" && detectPlaylistID(playlistID) {
		return &ParsedArg{
			ContentType:   ContentTypePlaylist,
			OriginalInput: originalURL,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/playlist?list=%s", playlistID),
			ID:            playlistID,
		}
	}

	return &ParsedArg{
		ContentType:   ContentTypeUnknown,
		OriginalInput: originalURL,
		Error:         fmt.Errorf("invalid playlist ID: %s", playlistID),
	}
}

// parseChannelURL handles /channel/UC... URLs
func parseChannelURL(u *url.URL, originalURL string) *ParsedArg {
	channelID := strings.TrimPrefix(u.Path, "/channel/")
	if detectChannelID(channelID) {
		return &ParsedArg{
			ContentType:   ContentTypeChannel,
			OriginalInput: originalURL,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/channel/%s", channelID),
			ID:            channelID,
		}
	}

	return &ParsedArg{
		ContentType:   ContentTypeUnknown,
		OriginalInput: originalURL,
		Error:         fmt.Errorf("invalid channel ID: %s", channelID),
	}
}

// parseHandleURL handles /@handle URLs
func parseHandleURL(u *url.URL, originalURL string) *ParsedArg {
	handle := strings.TrimPrefix(u.Path, "/")
	if detectChannelHandle(handle) {
		return &ParsedArg{
			ContentType:   ContentTypeChannel,
			OriginalInput: originalURL,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/%s", handle),
			ID:            handle,
		}
	}

	return &ParsedArg{
		ContentType:   ContentTypeUnknown,
		OriginalInput: originalURL,
		Error:         fmt.Errorf("invalid channel handle: %s", handle),
	}
}

// parseCustomChannelURL handles /c/ChannelName URLs
func parseCustomChannelURL(u *url.URL, originalURL string) *ParsedArg {
	channelName := strings.TrimPrefix(u.Path, "/c/")
	if channelName != "" && len(channelName) >= 3 && len(channelName) <= 30 {
		return &ParsedArg{
			ContentType:   ContentTypeChannel,
			OriginalInput: originalURL,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/c/%s", channelName),
			ID:            channelName,
		}
	}

	return &ParsedArg{
		ContentType:   ContentTypeUnknown,
		OriginalInput: originalURL,
		Error:         fmt.Errorf("invalid custom channel name: %s", channelName),
	}
}

// parseUserChannelURL handles /user/Username URLs (legacy)
func parseUserChannelURL(u *url.URL, originalURL string) *ParsedArg {
	username := strings.TrimPrefix(u.Path, "/user/")
	if username != "" && len(username) >= 3 && len(username) <= 30 {
		return &ParsedArg{
			ContentType:   ContentTypeChannel,
			OriginalInput: originalURL,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/user/%s", username),
			ID:            username,
		}
	}

	return &ParsedArg{
		ContentType:   ContentTypeUnknown,
		OriginalInput: originalURL,
		Error:         fmt.Errorf("invalid username: %s", username),
	}
}

// ParseArgNew is the enhanced argument parser that returns detailed information
func ParseArgNew(arg string) *ParsedArg {
	arg = strings.TrimSpace(arg)

	// Handle URLs
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		return parseYouTubeURL(arg)
	}

	// Handle bare IDs and handles
	contentType := detectContentType(arg)

	switch contentType {
	case ContentTypeVideo:
		return &ParsedArg{
			ContentType:   ContentTypeVideo,
			OriginalInput: arg,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/watch?v=%s", arg),
			ID:            arg,
		}

	case ContentTypePlaylist:
		return &ParsedArg{
			ContentType:   ContentTypePlaylist,
			OriginalInput: arg,
			NormalizedURL: fmt.Sprintf("https://www.youtube.com/playlist?list=%s", arg),
			ID:            arg,
		}

	case ContentTypeChannel:
		if detectChannelID(arg) {
			return &ParsedArg{
				ContentType:   ContentTypeChannel,
				OriginalInput: arg,
				NormalizedURL: fmt.Sprintf("https://www.youtube.com/channel/%s", arg),
				ID:            arg,
			}
		} else if detectChannelHandle(arg) {
			// Ensure @ prefix for handles
			handle := arg
			if !strings.HasPrefix(handle, "@") {
				handle = "@" + handle
			}
			return &ParsedArg{
				ContentType:   ContentTypeChannel,
				OriginalInput: arg,
				NormalizedURL: fmt.Sprintf("https://www.youtube.com/%s", handle),
				ID:            handle,
			}
		}

	case ContentTypeCommand:
		return &ParsedArg{
			ContentType:   ContentTypeCommand,
			OriginalInput: arg,
			Error:         fmt.Errorf("'%s' looks like a command, not YouTube content", arg),
		}

	default:
		return &ParsedArg{
			ContentType:   ContentTypeUnknown,
			OriginalInput: arg,
			Error:         fmt.Errorf("unable to determine content type for '%s'", arg),
		}
	}

	return &ParsedArg{
		ContentType:   ContentTypeUnknown,
		OriginalInput: arg,
		Error:         fmt.Errorf("unexpected parsing state for '%s'", arg),
	}
}

// ParseYouTubeArg parses and validates a YouTube content argument.
func ParseYouTubeArg(arg string) (YouTubeRef, error) {
	parsed := ParseArgNew(arg)
	if parsed.Error != nil {
		return YouTubeRef{}, parsed.Error
	}
	if !parsed.IsValid() {
		return YouTubeRef{}, fmt.Errorf("invalid YouTube content: %s", parsed.ContentType)
	}
	return newYouTubeRef(parsed), nil
}

// ParseVideoArg parses and validates a YouTube video argument.
func ParseVideoArg(arg string) (YouTubeRef, error) {
	return tldw.ParseVideoRef(arg)
}

func IsValidYouTubeID(id string) bool { return videoIDPattern.MatchString(id) }

func IsLikelyCommand(arg string) bool {
	return len(arg) <= 10 && !IsValidYouTubeID(arg) && !IsValidPlaylistID(arg)
}

func IsValidPlaylistID(id string) bool { return isValidPlaylistID(id) }

func isValidPlaylistID(id string) bool {
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

var validIDCharacters = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
