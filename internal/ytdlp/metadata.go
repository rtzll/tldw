package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

func (yt *YouTube) metadata(ctx context.Context, ref tldw.YouTubeRef) (*tldw.VideoMetadata, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Extracting video metadata...\n")
	}

	// Build arguments for yt-dlp command
	args := []string{
		"--skip-download",       // Don't download the actual video
		"--dump-single-json",    // Get all info in JSON format
		"--no-playlist",         // Don't process playlists
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
			yt.log.Printf("Metadata extraction error: %v\n", err)
			yt.log.Printf("Command output: %s\n", string(output))
		}
		return nil, fmt.Errorf("extracting video metadata: %w", err)
	}

	// Parse JSON once to populate metadata and caption availability
	var raw struct {
		tldw.VideoMetadata
		Uploader          string             `json:"uploader"`
		UploaderURL       string             `json:"uploader_url"`
		Creator           string             `json:"creator"`
		Creators          metadataStringList `json:"creators"`
		UploadDate        string             `json:"upload_date"`
		Subtitles         map[string]any     `json:"subtitles"`
		AutomaticCaptions map[string]any     `json:"automatic_captions"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		if yt.verbose {
			yt.log.Printf("Failed to parse metadata JSON: %v\n", err)
		}
		return nil, fmt.Errorf("parsing video metadata: %w", err)
	}

	languages := extractCaptionLanguages(raw.Subtitles, raw.AutomaticCaptions)
	metadata := raw.VideoMetadata
	metadata.HasCaptions = len(languages) > 0
	metadata.CaptionLanguages = languages
	metadata.Creators = bestMetadataCreators(raw.Creator, raw.Creators)
	metadata.Channel = bestMetadataChannel(metadata.Channel, raw.Uploader)
	metadata.ChannelURL = bestMetadataChannelURL(metadata.ChannelURL, raw.UploaderURL)
	metadata.PublishedAt = bestMetadataPublishedAt(metadata.PublishedAt, raw.UploadDate)

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Metadata extraction completed\n")
		yt.log.Printf("Title: %s\n", metadata.Title)
		yt.log.Printf("Channel: %s\n", metadata.Channel)
		yt.log.Printf("Duration: %.2f seconds\n", metadata.Duration)
	}

	return &metadata, nil
}

type metadataStringList []string

func (list *metadataStringList) UnmarshalJSON(data []byte) error {
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		*list = values
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*list = []string{value}
		return nil
	}

	if strings.TrimSpace(string(data)) == "null" {
		*list = nil
		return nil
	}

	return fmt.Errorf("metadata string list must be a string or string array")
}

func bestMetadataCreators(creator string, creators []string) []string {
	if cleaned := nonEmptyStrings(creators); len(cleaned) > 0 {
		return cleaned
	}

	if trimmed := strings.TrimSpace(creator); trimmed != "" {
		return []string{trimmed}
	}

	return nil
}

func bestMetadataChannel(channel, uploader string) string {
	for _, candidate := range []string{channel, uploader} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func bestMetadataChannelURL(channelURL, uploaderURL string) string {
	for _, candidate := range []string{channelURL, uploaderURL} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func bestMetadataPublishedAt(publishedAt, uploadDate string) string {
	if trimmed := strings.TrimSpace(publishedAt); trimmed != "" {
		return trimmed
	}

	trimmed := strings.TrimSpace(uploadDate)
	if len(trimmed) == 8 && isDigits(trimmed) {
		return fmt.Sprintf("%s-%s-%s", trimmed[:4], trimmed[4:6], trimmed[6:8])
	}

	return trimmed
}

func isDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
