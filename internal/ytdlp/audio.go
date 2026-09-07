package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtzll/tldw/internal/tldw"
)

// Audio gets mp3 audio from a YouTube video
func (yt *YouTube) audio(ctx context.Context, ref tldw.YouTubeRef) (string, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Downloading audio...\n")
	}

	// Create path in configured cache directory
	cacheDir := yt.cacheDir
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	outputFile := filepath.Join(cacheDir, ref.ID()+".mp3")
	if info, err := os.Stat(outputFile); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return outputFile, nil
	}
	workDir, err := os.MkdirTemp(cacheDir, "audio-")
	if err != nil {
		return "", fmt.Errorf("creating audio workspace: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			yt.log.Printf("Warning: failed to clean download workspace: %v\n", err)
		}
	}()
	outputPath := filepath.Join(workDir, "%(id)s.%(ext)s")

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
	downloaded := filepath.Join(workDir, ref.ID()+".mp3")
	if info, err := os.Stat(downloaded); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", fmt.Errorf("audio download produced no usable file")
	}
	if err := os.Rename(downloaded, outputFile); err != nil {
		return "", fmt.Errorf("caching audio: %w", err)
	}
	return outputFile, nil
}
