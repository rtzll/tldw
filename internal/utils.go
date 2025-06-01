package internal

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// ParseArg normalizes YouTube video IDs and URLs
func ParseArg(arg string) (string, string) {
	if strings.HasPrefix(arg, "https://") {
		videoID, err := getVideoID(arg)
		if err != nil {
			// Fall back to original arg if we can't extract ID
			return arg, arg
		}
		return arg, videoID
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
	supportedModels := []string{"gpt-4o", "gpt-4o-mini", "o4-mini"}
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
	// Short strings (1-10 chars) that don't look like YouTube IDs are likely commands
	if len(arg) <= 10 && !IsValidYouTubeID(arg) {
		return true
	}
	return false
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
