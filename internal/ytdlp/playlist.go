package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rtzll/tldw/internal/tldw"
)

type playlistEntry struct {
	ID string `json:"id"`
}

type playlistMetadata struct {
	Title   string          `json:"title"`
	Entries []playlistEntry `json:"entries"`
}

func (yt *Client) playlistVideoURLs(ctx context.Context, ref tldw.ContentRef) (*tldw.PlaylistInfo, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Extracting playlist video URLs...\n")
	}

	// Build arguments for yt-dlp command
	args := []string{
		"--flat-playlist",
		"--dump-single-json",
	}
	args = append(args, lookupArgs(ref)...)
	args = append(args, ref.URL())

	// Run the command
	output, err := yt.run(ctx, "playlist", ref, args, nil)
	if err != nil {
		if yt.verbose {
			yt.log.Printf("Playlist extraction error: %v\n", err)
			yt.log.Printf("Command output: %s\n", string(output))
		}
		return nil, fmt.Errorf("extracting playlist URLs: %w", err)
	}

	// Parse the JSON output
	var playlist playlistMetadata
	if err := json.Unmarshal(output, &playlist); err != nil {
		if yt.verbose {
			yt.log.Printf("Failed to parse playlist JSON: %v\n", err)
		}
		return nil, fmt.Errorf("parsing playlist metadata: %w", err)
	}

	// Extract video URLs
	var videos []tldw.ContentRef
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

	return &tldw.PlaylistInfo{
		Title:  playlist.Title,
		Videos: videos,
	}, nil
}
