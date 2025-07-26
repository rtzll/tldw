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
	"golang.org/x/term"
)

// ParseArg normalizes YouTube video IDs and URLs, now also handles playlists
func ParseArg(arg string) (string, string) {
	if strings.HasPrefix(arg, "https://") {
		// Try video ID first - prioritize individual videos over playlists
		videoID, err := getVideoID(arg)
		if err == nil {
			// Successfully extracted video ID, use it even if URL also has playlist
			return arg, videoID
		}

		// No video ID found, check if it's a playlist URL
		if strings.Contains(arg, "list=") {
			playlistID, err := getPlaylistID(arg)
			if err != nil {
				return arg, arg
			}
			return arg, playlistID
		}

		// Fall back to original arg if we can't extract either
		return arg, arg
	}

	// Check if the arg looks like a playlist ID
	if IsValidPlaylistID(arg) {
		return "https://www.youtube.com/playlist?list=" + arg, arg
	}

	return "https://www.youtube.com/watch?v=" + arg, arg
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
	supportedModels := []string{"gpt-4o", "gpt-4o-mini", "o4-mini", "gpt-4.1-nano"}
	if slices.Contains(supportedModels, model) {
		return nil
	}
	return fmt.Errorf("unsupported model: %s (supported: %s)", model, strings.Join(supportedModels, ", "))
}

// EnsureDirs creates directories if needed
func EnsureDirs(dir ...string) error {
	for _, dir := range dir {
		if !FileExists(dir) {
			return os.MkdirAll(dir, 0755)
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
	// Common playlist prefixes: PL, UU, FL, RD, etc.
	playlistPrefixes := []string{"PL", "UU", "FL", "RD", "LP", "BP", "QL", "SV", "EL", "LL", "UC"}

	// Check for standard prefixes with appropriate lengths
	for _, prefix := range playlistPrefixes {
		if strings.HasPrefix(id, prefix) {
			// Standard playlist IDs are typically 16, 32, or 34 characters
			if len(id) == 18 || len(id) == 34 || len(id) == 36 {
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
func getPlaylistID(youtubeURL string) (string, error) {
	youtubeURL = strings.TrimSpace(youtubeURL)
	u, err := url.Parse(youtubeURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}

	if u.Host != "www.youtube.com" && u.Host != "youtube.com" {
		return "", fmt.Errorf("not a YouTube URL: %s", youtubeURL)
	}

	if list := u.Query().Get("list"); list != "" {
		if IsValidPlaylistID(list) {
			return list, nil
		}
		return "", fmt.Errorf("invalid playlist ID format: %s", list)
	}

	return "", fmt.Errorf("could not extract playlist ID from URL: %s", youtubeURL)
}

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
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Channel     string         `json:"channel"`
	Duration    float64        `json:"duration"`
	Categories  []string       `json:"categories"`
	Tags        []string       `json:"tags"`
	Chapters    []VideoChapter `json:"chapters"`
	HasCaptions bool           `json:"has_captions"`
	CachedAt    time.Time      `json:"cached_at"`
}

// SaveMetadata saves video metadata to cache as JSON
func SaveMetadata(youtubeID string, metadata *VideoMetadata, transcriptsDir string) error {
	cached := CachedVideoMetadata{
		Title:       metadata.Title,
		Description: metadata.Description,
		Channel:     metadata.Channel,
		Duration:    metadata.Duration,
		Categories:  metadata.Categories,
		Tags:        metadata.Tags,
		Chapters:    metadata.Chapters,
		HasCaptions: metadata.HasCaptions,
		CachedAt:    time.Now(),
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
		Title:       cached.Title,
		Description: cached.Description,
		Channel:     cached.Channel,
		Duration:    cached.Duration,
		Categories:  cached.Categories,
		Tags:        cached.Tags,
		Chapters:    cached.Chapters,
		HasCaptions: cached.HasCaptions,
	}, nil
}
