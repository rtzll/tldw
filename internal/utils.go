package internal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
	"github.com/openai/openai-go/v2"
	"golang.org/x/term"
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
	// Examples: gpt-4o, gpt-4.1-nano, gpt-5, gpt-5-mini, gpt-5-nano, gpt-5-chat-latest
	modelNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,}$`)
)

// Content type detection functions

// detectVideoID checks if a string looks like a YouTube video ID
func detectVideoID(s string) bool {
	return videoIDPattern.MatchString(s)
}

// detectPlaylistID checks if a string looks like a YouTube playlist ID
func detectPlaylistID(s string) bool {
	// Only regular playlists (PL) - pattern already includes length validation
	return playlistIDPattern.MatchString(s)
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

// ParseArg maintains backward compatibility with the old (string, string) signature
func ParseArg(arg string) (string, string) {
	parsed := ParseArgNew(arg)

	if parsed.Error != nil {
		// For backward compatibility, return original input for errors
		return arg, arg
	}

	return parsed.NormalizedURL, parsed.ID
}

// VideoIDExtractor extracts video IDs from YouTube URLs
type VideoIDExtractor func(string) (string, error)

// Default implementation of video ID extraction
var getVideoID VideoIDExtractor = func(youtubeURL string) (string, error) {
	// Trim any leading or trailing whitespace from the URL
	youtubeURL = strings.TrimSpace(youtubeURL)
	u, err := url.Parse(youtubeURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}

	if u.Host != "www.youtube.com" && u.Host != "youtube.com" && u.Host != "youtu.be" {
		return "", fmt.Errorf("not a YouTube URL: %s", youtubeURL)
	}

	if v := u.Query().Get("v"); v != "" {
		return v, nil
	}

	// Don't extract video IDs from playlist URLs
	if strings.Contains(u.Path, "/playlist") {
		return "", fmt.Errorf("this is a playlist URL, not a video URL: %s", youtubeURL)
	}

	parts := strings.Split(u.Path, "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1], nil
	}

	return "", fmt.Errorf("could not extract video ID from URL: %s", youtubeURL)
}

// AskUser is a variable that holds the function for asking user confirmation
// This allows it to be replaced in tests
var AskUser = func(message string) bool {
	fmt.Printf("%s (y/N): ", message)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		response := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return strings.HasPrefix(response, "y")
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
	}
	return false
}

// CleanupTempDir purges files from a temporary directory
func CleanupTempDir(tempDir string) error {
	// Check if directory exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		return nil // Directory doesn't exist, nothing to clean up
	}

	// Read directory contents
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return fmt.Errorf("reading temp directory: %w", err)
	}

	// Remove each file in the directory
	for _, entry := range entries {
		filePath := filepath.Join(tempDir, entry.Name())
		if err := os.Remove(filePath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temporary file %s: %v\n", filePath, err)
		}
	}

	// Try to remove the directory itself
	if err := os.Remove(tempDir); err != nil {
		// It's okay if we can't remove the directory (it might not be empty)
		// Just log a warning
		fmt.Fprintf(os.Stderr, "Note: could not remove temp directory %s: %v\n", tempDir, err)
	}

	return nil
}

// getTerminalWidth gets terminal width with fallback
func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80
	}

	if width > 10 {
		return width - 4
	}

	return width
}

// RenderMarkdown renders markdown content with glamour
func RenderMarkdown(content string) (string, error) {
	width := getTerminalWidth()
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
		glamour.WithColorProfile(termenv.EnvColorProfile()),
	)
	if err != nil {
		return "", fmt.Errorf("creating terminal renderer: %w", err)
	}

	renderedContent, err := r.Render(content)
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}

	return renderedContent, nil
}

// FileExists checks if a file exists
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

// ValidateModel checks if the model is supported
func ValidateModel(model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("model cannot be empty")
	}
	if !modelNamePattern.MatchString(model) {
		return fmt.Errorf("invalid model format: %s (allowed: lowercase letters, digits, dot, underscore, hyphen)", model)
	}
	supported := []openai.ChatModel{
		openai.ChatModelO1,
		openai.ChatModelO1Mini,
		openai.ChatModelO3,
		openai.ChatModelO3Mini,
		openai.ChatModelO4Mini,
		openai.ChatModelGPT4o,
		openai.ChatModelGPT4oMini,
		openai.ChatModelGPT4_1,
		openai.ChatModelGPT4_1Mini,
		openai.ChatModelGPT4_1Nano,
		openai.ChatModelGPT5,
		openai.ChatModelGPT5Mini,
		openai.ChatModelGPT5Nano,
	}
	if slices.Contains(supported, openai.ChatModel(model)) {
		return nil
	}
	supportedStrings := make([]string, 0, len(supported))
	for _, m := range supported {
		supportedStrings = append(supportedStrings, string(m))
	}
	return fmt.Errorf("unsupported model: %s (supported: %s)", model, strings.Join(supportedStrings, ", "))
}

// EnsureDirs creates directories if needed
func EnsureDirs(dirs ...string) error {
	for _, d := range dirs {
		if !FileExists(d) {
			if err := os.MkdirAll(d, 0755); err != nil {
				return err
			}
		}
	}
	return nil
}

// cleanupFiles removes temporary files
func cleanupFiles(files ...string) {
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove file %s: %v\n", file, err)
		}
	}
}

// IsValidYouTubeID checks if a string looks like a valid YouTube video ID
func IsValidYouTubeID(id string) bool {
	// YouTube video IDs are exactly 11 characters long
	if len(id) != 11 {
		return false
	}

	// YouTube video IDs contain only alphanumeric characters, hyphens, and underscores
	matched, _ := regexp.MatchString(`^[A-Za-z0-9_-]+$`, id)
	return matched
}

// IsLikelyCommand checks if a string looks like it might be a mistyped command
func IsLikelyCommand(arg string) bool {
	// Short strings (1-10 chars) that don't look like YouTube IDs or playlist IDs are likely commands
	if len(arg) <= 10 && !IsValidYouTubeID(arg) && !IsValidPlaylistID(arg) {
		return true
	}
	return false
}

// IsValidPlaylistID checks if a string looks like a valid YouTube playlist ID
func IsValidPlaylistID(id string) bool {
	// For PL-prefixed playlists (regular user playlists), use the same logic as detectPlaylistID
	if strings.HasPrefix(id, "PL") {
		return detectPlaylistID(id)
	}

	// Common non-PL playlist prefixes: UU, FL, RD, etc.
	nonPLPrefixes := []string{"UU", "FL", "RD", "LP", "BP", "QL", "SV", "EL", "LL", "UC"}

	// Check for standard non-PL prefixes with appropriate lengths
	for _, prefix := range nonPLPrefixes {
		if strings.HasPrefix(id, prefix) {
			// Standard playlist IDs are typically 18, 34 characters total
			if len(id) == 18 || len(id) == 34 {
				matched, _ := regexp.MatchString(`^[A-Za-z0-9_-]+$`, id)
				return matched
			}
		}
	}

	// Check for music playlists (OLAK5uy_, RDCLAK5uy_)
	if strings.HasPrefix(id, "OLAK5uy_") || strings.HasPrefix(id, "RDCLAK5uy_") {
		if len(id) == 40 {
			matched, _ := regexp.MatchString(`^[A-Za-z0-9_-]+$`, id)
			return matched
		}
	}

	return false
}

// getPlaylistID extracts playlist ID from YouTube URLs
// ValidateOpenAIAPIKey checks if the OpenAI API key is set and returns a standardized error if not
func ValidateOpenAIAPIKey(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("OpenAI API key is required - set it in config.toml or OPENAI_API_KEY environment variable")
	}
	return nil
}

// SaveTranscript saves a transcript to the specified directory with standard error handling
func SaveTranscript(youtubeID, transcript, transcriptsDir string) error {
	transcriptPath := filepath.Join(transcriptsDir, youtubeID+".txt")
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0644); err != nil {
		return fmt.Errorf("saving transcript: %w", err)
	}
	return nil
}

// CachedVideoMetadata extends VideoMetadata with cache information
type CachedVideoMetadata struct {
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Channel          string         `json:"channel"`
	Duration         float64        `json:"duration"`
	Categories       []string       `json:"categories"`
	Tags             []string       `json:"tags"`
	Chapters         []VideoChapter `json:"chapters"`
	HasCaptions      bool           `json:"has_captions"`
	CaptionLanguages []string       `json:"caption_languages"`
	CachedAt         time.Time      `json:"cached_at"`
}

// SaveMetadata saves video metadata to cache as JSON
func SaveMetadata(youtubeID string, metadata *VideoMetadata, transcriptsDir string) error {
	cached := CachedVideoMetadata{
		Title:            metadata.Title,
		Description:      metadata.Description,
		Channel:          metadata.Channel,
		Duration:         metadata.Duration,
		Categories:       metadata.Categories,
		Tags:             metadata.Tags,
		Chapters:         metadata.Chapters,
		HasCaptions:      metadata.HasCaptions,
		CaptionLanguages: metadata.CaptionLanguages,
		CachedAt:         time.Now(),
	}

	metadataPath := filepath.Join(transcriptsDir, youtubeID+".meta.json")
	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("saving metadata: %w", err)
	}

	return nil
}

// LoadCachedMetadata loads video metadata from cache
func LoadCachedMetadata(youtubeID, transcriptsDir string) (*VideoMetadata, error) {
	metadataPath := filepath.Join(transcriptsDir, youtubeID+".meta.json")

	if !FileExists(metadataPath) {
		return nil, fmt.Errorf("metadata cache not found")
	}

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("reading metadata cache: %w", err)
	}

	var cached CachedVideoMetadata
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("parsing metadata cache: %w", err)
	}

	return &VideoMetadata{
		Title:            cached.Title,
		Description:      cached.Description,
		Channel:          cached.Channel,
		Duration:         cached.Duration,
		Categories:       cached.Categories,
		Tags:             cached.Tags,
		Chapters:         cached.Chapters,
		HasCaptions:      cached.HasCaptions,
		CaptionLanguages: cached.CaptionLanguages,
	}, nil
}
