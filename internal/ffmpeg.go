package internal

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// Audio handles audio file operations using FFmpeg
type Audio struct {
	cmdRunner CommandRunner
	tempDir   string
	verbose   bool
}

// NewAudio creates a new audio processor
func NewAudio(cmdRunner CommandRunner, tempDir string, verbose bool) *Audio {
	return &Audio{
		cmdRunner: cmdRunner,
		tempDir:   tempDir,
		verbose:   verbose,
	}
}

// Duration returns the audio file duration in seconds
func (a *Audio) Duration(ctx context.Context, audioFile string) (float64, error) {
	output, err := a.cmdRunner.Run(ctx, "ffprobe",
		"-i", audioFile,
		"-show_entries", "format=duration",
		"-v", "quiet",
		"-of", "csv=p=0")

	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w\nOutput: %s", err, string(output))
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("parsing duration: %w", err)
	}

	return duration, nil
}

// Split divides an audio file into smaller chunks
func (a *Audio) Split(ctx context.Context, audioFile string, numChunks int) ([]string, error) {
	if err := EnsureDirs(a.tempDir); err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}

	duration, err := a.Duration(ctx, audioFile)
	if err != nil {
		return nil, fmt.Errorf("getting audio duration: %w", err)
	}

	chunkDuration := int(math.Ceil(duration / float64(numChunks)))
	chunks := make([]string, 0, numChunks)

	for i := range numChunks {
		start := i * chunkDuration
		output := filepath.Join(a.tempDir, fmt.Sprintf("%s_chunk_%d.mp3", filepath.Base(audioFile), i))

		if err := a.Chunk(ctx, audioFile, start, chunkDuration, output); err != nil {
			cleanupFiles(chunks...)
			return nil, fmt.Errorf("creating chunk %d: %w", i, err)
		}
		chunks = append(chunks, output)
	}

	return chunks, nil
}

// Chunk extracts a segment from an audio file
func (a *Audio) Chunk(ctx context.Context, audioFile string, start, duration int, output string) error {
	cmdOutput, err := a.cmdRunner.Run(ctx, "ffmpeg",
		"-v", "quiet",
		"-i", audioFile,
		"-ss", strconv.Itoa(start),
		"-t", strconv.Itoa(duration),
		"-c:a", "copy",
		"-y", output)

	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(cmdOutput))
	}
	return nil
}
