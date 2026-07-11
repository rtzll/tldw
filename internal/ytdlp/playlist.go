package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rtzll/tldw/internal/tldw"
)

// PlaylistEntry represents a single video in a playlist
type PlaylistEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// PlaylistMetadata contains YouTube playlist information
type PlaylistMetadata struct {
	Title   string          `json:"title"`
	Entries []PlaylistEntry `json:"entries"`
}

// PlaylistVideoURLs fetches all video URLs from a YouTube playlist
func (yt *YouTube) playlistVideoURLs(ctx context.Context, ref YouTubeRef) (*PlaylistInfo, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Extracting playlist video URLs...\n")
	}

	// Build arguments for yt-dlp command
	args := []string{
		"--flat-playlist",       // Only extract video URLs, don't download
		"--dump-single-json",    // Get all info in JSON format
		"--sleep-interval", "1", // Sleep 1-3 seconds between requests to avoid rate limiting
		"--max-sleep-interval", "3",
		"--extractor-args", "youtube:player_client=web,android,-tv", // Exclude DRM-protected TV client
		"-q", // Quiet mode
		ref.URL(),
	}

	// Run the command
	output, err := yt.executor.Run(ctx, "yt-dlp", args...)
	if err != nil {
		if yt.verbose {
			yt.log.Printf("Playlist extraction error: %v\n", err)
			yt.log.Printf("Command output: %s\n", string(output))
		}
		return nil, fmt.Errorf("extracting playlist URLs: %w", err)
	}

	// Parse the JSON output
	var playlist PlaylistMetadata
	if err := json.Unmarshal(output, &playlist); err != nil {
		if yt.verbose {
			yt.log.Printf("Failed to parse playlist JSON: %v\n", err)
		}
		return nil, fmt.Errorf("parsing playlist metadata: %w", err)
	}

	// Extract video URLs
	var videos []YouTubeRef
	for _, entry := range playlist.Entries {
		if tldw.IsValidVideoID(entry.ID) {
			ref, err := tldw.ParseVideoRef(entry.ID)
			if err == nil {
				videos = append(videos, ref)
			}
		}
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Found %d videos in playlist: %s\n", len(videos), playlist.Title)
	}

	return &PlaylistInfo{
		Title:  playlist.Title,
		Videos: videos,
	}, nil
}
