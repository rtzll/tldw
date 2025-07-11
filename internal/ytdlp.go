package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

// ErrDownloadFailed indicates a retryable download failure from yt-dlp
var ErrDownloadFailed = errors.New("yt-dlp download failed")

// VideoMetadata contains YouTube video information
type VideoMetadata struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Channel     string         `json:"channel"`
	Duration    float64        `json:"duration"`
	Categories  []string       `json:"categories"`
	Tags        []string       `json:"tags"`
	Chapters    []VideoChapter `json:"chapters"`
	HasCaptions bool           `json:"has_captions"`
}

// VideoChapter represents a video chapter marker
type VideoChapter struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Title     string  `json:"title"`
}

// YouTube handles YouTube video and transcript operations
type YouTube struct {
	fs             fs.FS
	transcriptsDir string
	verbose        bool
	cmdRunner      CommandRunner
}

// NewYouTube creates a new YouTube downloader
func NewYouTube(filesystem fs.FS, transcriptsDir string, verbose bool) *YouTube {
	return &YouTube{
		fs:             filesystem,
		transcriptsDir: transcriptsDir,
		verbose:        verbose,
		cmdRunner:      &DefaultCommandRunner{},
	}
}

// Metadata fetches video details using direct yt-dlp command execution
func (yt *YouTube) Metadata(ctx context.Context, youtubeURL string) (*VideoMetadata, error) {
	if yt.verbose {
		fmt.Println("Extracting video metadata...")
	}

	// Build arguments for yt-dlp command
	args := []string{
		"--skip-download",    // Don't download the actual video
		"--dump-single-json", // Get all info in JSON format
		"--no-playlist",      // Don't process playlists
		"-q",                 // Quiet mode
		youtubeURL,
	}

	// Run the command
	output, err := yt.cmdRunner.Run(ctx, "yt-dlp", args...)
	if err != nil {
		if yt.verbose {
			fmt.Printf("Metadata extraction error: %v\n", err)
			fmt.Printf("Command output: %s\n", string(output))
		}
		return nil, fmt.Errorf("extracting video metadata: %w", err)
	}

	// Parse the JSON output into a raw map first to extract subtitle info
	var rawData map[string]any
	if err := json.Unmarshal(output, &rawData); err != nil {
		if yt.verbose {
			fmt.Printf("Failed to parse metadata JSON: %v\n", err)
		}
		return nil, fmt.Errorf("parsing video metadata: %w", err)
	}

	// Parse the JSON output into our struct
	var metadata VideoMetadata
	if err := json.Unmarshal(output, &metadata); err != nil {
		if yt.verbose {
			fmt.Printf("Failed to parse metadata JSON: %v\n", err)
		}
		return nil, fmt.Errorf("parsing video metadata: %w", err)
	}

	// Extract subtitle availability information
	metadata.HasCaptions = extractSubtitleInfo(rawData)

	if yt.verbose {
		fmt.Println("Metadata extraction completed")
		fmt.Printf("Title: %s\n", metadata.Title)
		fmt.Printf("Channel: %s\n", metadata.Channel)
		fmt.Printf("Duration: %.2f seconds\n", metadata.Duration)
	}

	return &metadata, nil
}

// Audio gets mp3 audio from a YouTube video
func (yt *YouTube) Audio(ctx context.Context, youtubeURL string) (string, error) {
	if yt.verbose {
		fmt.Println("Downloading audio...")
	}

	// Extract video ID to construct the output filename
	videoID, err := getVideoID(youtubeURL)
	if err != nil {
		return "", fmt.Errorf("extracting video ID: %w", err)
	}

	// Create path in XDG cache directory
	cacheDir := filepath.Join(xdg.CacheHome, "tldw")
	if err := EnsureDirs(cacheDir); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	// Set output path in cache directory
	outputPath := filepath.Join(cacheDir, "%(id)s.%(ext)s")

	// Build arguments for yt-dlp command
	args := []string{
		"-f", "bestaudio", // Select best audio format
		"--extract-audio",       // Extract audio from video
		"--audio-format", "mp3", // Convert to MP3 format
		"--audio-quality", "10", // Set audio quality (0 is best, 10 is worst)
		"-o", outputPath, // Output to XDG cache directory
		youtubeURL, // The YouTube URL or ID
	}

	// Run the command
	output, err := yt.cmdRunner.Run(ctx, "yt-dlp", args...)
	if err != nil {
		if yt.verbose {
			fmt.Printf("Audio download error: %v\n", err)
			fmt.Printf("Command output: %s\n", string(output))
		}
		return "", fmt.Errorf("yt-dlp failed: %w\nOutput: %s", err, string(output))
	}

	if yt.verbose {
		fmt.Println("Audio download completed")
	}

	// Return the full path to the downloaded file
	outputFile := filepath.Join(cacheDir, videoID+".mp3")
	return outputFile, nil
}

// Transcript fetches subtitles using yt-dlp
func (yt *YouTube) Transcript(ctx context.Context, youtubeURL string) error {
	if yt.verbose {
		fmt.Println("Downloading subtitles...")
	}

	// Get video ID for checking files
	videoID, err := getVideoID(youtubeURL)
	if err != nil {
		return fmt.Errorf("failed to extract video ID: %w", err)
	}

	// Create path in XDG cache directory
	cacheDir := filepath.Join(xdg.CacheHome, "tldw")
	if err := EnsureDirs(cacheDir); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	// Set output path in cache directory
	outputPath := filepath.Join(cacheDir, "%(id)s")

	// Build arguments for yt-dlp command
	args := []string{
		"--write-subs",      // Enable subtitle writing
		"--write-auto-subs", // Enable auto-generated subtitle writing
		"--sub-langs", "en", // Download all English subtitle variants
		"--convert-subs", "srt", // Convert subtitles to SRT format
		"--skip-download", // Skip downloading the video
		"-o", outputPath,  // Output to XDG cache directory
		youtubeURL, // The YouTube URL or ID
	}

	// Run the command
	output, err := yt.cmdRunner.Run(ctx, "yt-dlp", args...)
	if err != nil {
		if yt.verbose {
			fmt.Printf("Subtitle download error: %v\n", err)
			fmt.Printf("Command output: %s\n", string(output))
		}
		return fmt.Errorf("%w: %v", ErrDownloadFailed, err)
	}

	if yt.verbose {
		fmt.Println("Subtitle download completed")
	}

	// Check for the downloaded subtitle files
	pattern := filepath.Join(cacheDir, fmt.Sprintf("%s*.srt", videoID))
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		if yt.verbose {
			fmt.Println("No subtitle files found after download")
			fmt.Printf("Searched for pattern: %s\n", pattern)
		}
		return fmt.Errorf("no subtitle files found after download")
	}

	if yt.verbose {
		fmt.Printf("Found %d subtitle file(s): %v\n", len(files), files)
	}

	return nil
}

// FetchTranscript gets a transcript, using cached version if available
func (yt *YouTube) FetchTranscript(ctx context.Context, youtubeURL string) (string, error) {
	youtubeID, err := getVideoID(youtubeURL)
	if err != nil {
		return "", fmt.Errorf("extracting video ID: %w", err)
	}

	if yt.verbose {
		fmt.Printf("Looking for existing transcript for video ID: %s\n", youtubeID)
	}

	// Look for an existing transcript first
	transcriptPath, err := yt.findExistingTranscript(youtubeID)
	if err != nil {
		return "", fmt.Errorf("error searching for existing transcript: %w", err)
	}

	if transcriptPath != "" {
		if yt.verbose {
			fmt.Printf("Found existing transcript: %s\n", transcriptPath)
		}
		// Process the existing transcript
		return yt.processSrtTranscript(transcriptPath)
	}

	if yt.verbose {
		fmt.Println("No existing transcript found, attempting to download...")
	}

	// No existing transcript found, try to download one
	err = yt.Transcript(ctx, youtubeURL)
	if err != nil {
		// Preserve the error type for retry logic
		return "", err
	}

	// Look for the downloaded transcript
	transcriptPath, err = yt.findExistingTranscript(youtubeID)
	if err != nil || transcriptPath == "" {
		if yt.verbose {
			fmt.Printf("Could not find downloaded transcript: %v\n", err)
		}
		return "", fmt.Errorf("downloaded transcript not found")
	}

	if yt.verbose {
		fmt.Printf("Successfully downloaded transcript: %s\n", transcriptPath)
	}

	return yt.processSrtTranscript(transcriptPath)
}

// findExistingTranscript locates a previously downloaded transcript
func (yt *YouTube) findExistingTranscript(videoID string) (string, error) {
	// Look in XDG cache directory
	cacheDir := filepath.Join(xdg.CacheHome, "tldw")
	if FileExists(cacheDir) {
		cacheFiles, err := os.ReadDir(cacheDir)
		if err == nil {
			for _, entry := range cacheFiles {
				name := entry.Name()
				if strings.HasPrefix(name, videoID) && strings.HasSuffix(name, ".srt") {
					return filepath.Join(cacheDir, name), nil
				}
			}
		}
	}

	// Look in transcripts directory for already processed transcripts
	if FileExists(yt.transcriptsDir) {
		transcriptFiles, err := os.ReadDir(yt.transcriptsDir)
		if err == nil {
			for _, entry := range transcriptFiles {
				name := entry.Name()
				if strings.HasPrefix(name, videoID) && strings.HasSuffix(name, ".srt") {
					return filepath.Join(yt.transcriptsDir, name), nil
				}
			}
		}
	}

	return "", nil
}

// processSrtTranscript converts SRT to clean plain text
func (yt *YouTube) processSrtTranscript(filePath string) (string, error) {
	if yt.verbose {
		fmt.Printf("Processing SRT transcript: %s\n", filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("reading SRT file: %w", err)
	}

	lines := parseSRT(string(content))

	var sb strings.Builder
	deduplicatedLines := removeDuplicates(lines)
	for i, line := range deduplicatedLines {
		sb.WriteString(line)
		if i < len(deduplicatedLines)-1 {
			sb.WriteString("\n")
		}
	}
	text := strings.TrimSpace(sb.String())

	// Extract video ID from filename
	id := strings.Split(filepath.Base(filePath), ".")[0]

	// Save to transcripts directory (for permanent storage)
	if err := SaveTranscript(id, text, yt.transcriptsDir); err != nil {
		return "", err
	}

	// If the file is in the cache directory, remove it after processing
	cacheDir := filepath.Join(xdg.CacheHome, "tldw")
	if strings.HasPrefix(filePath, cacheDir) && FileExists(filePath) {
		if err := os.Remove(filePath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove SRT file from cache: %v\n", err)
		}
	}

	return text, nil
}

// parseSRT extracts text content from SRT format
func parseSRT(content string) []string {
	var lines []string

	for block := range strings.SplitSeq(content, "\n\n") {
		blockLines := strings.Split(block, "\n")
		if len(blockLines) >= 3 {
			// Skip sequence number and timestamp, get text lines
			for i := 2; i < len(blockLines); i++ {
				if strings.TrimSpace(blockLines[i]) != "" {
					lines = append(lines, strings.TrimSpace(blockLines[i]))
				}
			}
		}
	}

	return lines
}

// removeDuplicates eliminates consecutive repeated lines
func removeDuplicates(lines []string) []string {
	result := make([]string, 0, len(lines))
	prevLine := ""

	for _, line := range lines {
		isDuplicate := prevLine != "" && (strings.Contains(line, prevLine) || strings.Contains(prevLine, line))
		if !isDuplicate {
			result = append(result, line)
		}
		prevLine = line
	}

	return result
}

// extractSubtitleInfo extracts subtitle availability from yt-dlp JSON output
func extractSubtitleInfo(rawData map[string]any) bool {
	// Check for manual subtitles
	if subtitles, ok := rawData["subtitles"].(map[string]any); ok && subtitles != nil {
		if len(subtitles) > 0 {
			return true
		}
	}

	// Check for automatic captions
	if autoCaptions, ok := rawData["automatic_captions"].(map[string]any); ok && autoCaptions != nil {
		if len(autoCaptions) > 0 {
			return true
		}
	}

	return false
}
