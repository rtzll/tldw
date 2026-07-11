package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Audio gets mp3 audio from a YouTube video
func (yt *YouTube) audio(ctx context.Context, ref YouTubeRef) (string, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Downloading audio...\n")
	}

	// Create path in configured cache directory
	cacheDir := yt.cacheDir
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
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
		ref.URL(), // The YouTube URL
	}

	output, err := yt.executor.Run(ctx, "yt-dlp", args...)
	if err != nil {
		if yt.verbose {
			yt.log.Printf("Audio download error: %v\n", err)
			yt.log.Printf("Command output: %s\n", string(output))
		}
		return "", fmt.Errorf("yt-dlp failed: %w\nOutput: %s", err, string(output))
	}
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Audio download completed\n")
	}

	// Return the full path to the downloaded file
	outputFile := filepath.Join(cacheDir, ref.ID()+".mp3")
	return outputFile, nil
}
