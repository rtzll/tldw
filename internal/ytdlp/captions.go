package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func getVideoID(youtubeURL string) (string, error) {
	parsed, err := tldw.ParseVideoRef(youtubeURL)
	if err != nil {
		return "", err
	}
	return parsed.ID(), nil
}

func (yt *YouTube) metadata(ctx context.Context, youtubeURL string) (*VideoMetadata, error) {
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
		youtubeURL,
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
		VideoMetadata
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
	metadata.Channel = bestMetadataChannel(metadata.Channel, raw.Uploader, raw.Creator, metadata.Creators)
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

func bestMetadataChannel(channel, uploader, creator string, creators []string) string {
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

// Audio gets mp3 audio from a YouTube video
func (yt *YouTube) audio(ctx context.Context, youtubeURL string) (string, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Downloading audio...\n")
	}

	// Extract video ID to construct the output filename
	videoID, err := getVideoID(youtubeURL)
	if err != nil {
		return "", fmt.Errorf("extracting video ID: %w", err)
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
		youtubeURL, // The YouTube URL or ID
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
	outputFile := filepath.Join(cacheDir, videoID+".mp3")
	return outputFile, nil
}

const (
	subLangsFlag             = "--sub-langs"
	englishFallbackSubLangs  = "en.*,en"
	maxPreferredCaptionLangs = 5
)

var englishCaptionPreference = []string{"en-US", "en", "en-GB", "en-CA", "en-AU", "en-NZ", "en-orig"}

var (
	assOverrideTagRegex = regexp.MustCompile(`\{\\[^}]*\}`)
	htmlTagRegex        = regexp.MustCompile(`<[^>]+>`)
)

// setSubLangsArg updates the value that follows the --sub-langs flag in-place
func setSubLangsArg(args []string, value string) error {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == subLangsFlag {
			args[i+1] = value
			return nil
		}
	}
	return fmt.Errorf("sub-langs flag %q not found", subLangsFlag)
}

// buildSubLangs selects the primary --sub-langs value and an optional fallback.
// The fallback is the broad English wildcard, used when the primary language
// set does not yield any files.
func buildSubLangs(preferred []string, originalLang string) (primary string, fallback string) {
	if len(preferred) == 0 {
		return englishFallbackSubLangs, ""
	}

	langs := prioritizeCaptionLanguages(preferred, originalLang)

	if len(langs) == 0 {
		return englishFallbackSubLangs, ""
	}

	primary = strings.Join(langs, ",")

	if primary == englishFallbackSubLangs {
		return primary, ""
	}

	return primary, englishFallbackSubLangs
}

// prioritizeCaptionLanguages deduplicates languages, prefers English variants,
// and limits the count to avoid enormous --sub-langs lists that can trigger rate limits.
func prioritizeCaptionLanguages(preferred []string, originalLang string) []string {
	seen := make(map[string]struct{})
	var cleaned []string

	for _, lang := range preferred {
		lang = strings.TrimSpace(lang)
		if lang == "" || lang == "live_chat" {
			continue
		}
		if _, exists := seen[lang]; exists {
			continue
		}
		seen[lang] = struct{}{}
		cleaned = append(cleaned, lang)
	}

	// Put English variants first if present.
	var ordered []string
	for _, lang := range englishCaptionPreference {
		if _, ok := seen[lang]; ok {
			ordered = append(ordered, lang)
		}
	}

	// If we found any English variants, return just the first match to keep
	// requests minimal.
	if len(ordered) > 0 {
		return ordered[:1]
	}

	// If we have a declared original language and it exists in the captions,
	// prefer that single language.
	if originalLang != "" {
		if _, ok := seen[originalLang]; ok {
			return []string{originalLang}
		}
	}

	// Otherwise, take the first non-English language (if any) to keep the list
	// to a single entry.
	for _, lang := range cleaned {
		ordered = append(ordered, lang)
		break
	}

	return ordered
}

// Transcript fetches subtitles using yt-dlp
// preferredLangs allows us to target known caption languages (from metadata) instead of hardcoding English.
// originalLang is the video's declared language; when English captions are absent we prefer this.
func (yt *YouTube) Transcript(ctx context.Context, youtubeURL string, preferredLangs []string, originalLang string) error {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Downloading subtitles...\n")
	}

	// Get video ID for checking files
	videoID, err := getVideoID(youtubeURL)
	if err != nil {
		return fmt.Errorf("failed to extract video ID: %w", err)
	}

	// Create path in configured cache directory
	cacheDir := yt.cacheDir
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	// Set output path in cache directory
	outputPath := filepath.Join(cacheDir, "%(id)s")
	pattern := filepath.Join(cacheDir, fmt.Sprintf("%s*.srt", videoID))

	primarySubLangs, fallbackSubLangs := buildSubLangs(preferredLangs, originalLang)

	args := []string{
		"--write-subs",      // Enable subtitle writing
		"--write-auto-subs", // Enable auto-generated subtitle writing
		"--sub-langs", primarySubLangs,
		"--convert-subs", "srt", // Convert subtitles to SRT format
		"--skip-download",       // Skip downloading the video
		"--sleep-interval", "2", // Sleep 2-5 seconds between requests to avoid rate limiting
		"--max-sleep-interval", "5",
		"--extractor-args", "youtube:player_client=web,android,-tv",
		"-o", outputPath, // Output to XDG cache directory
		youtubeURL,
	}

	// Run the command (and optional fallback)
	output, err := yt.executor.Run(ctx, "yt-dlp", args...)
	attemptedFallback := false
	if err != nil {
		// If subtitles were partially downloaded, allow success
		if files, globErr := filepath.Glob(pattern); globErr == nil && len(files) > 0 {
			if yt.verbose && !yt.quiet {
				yt.log.Printf("Subtitles downloaded despite errors (%s): %v\n", primarySubLangs, files)
			}
			err = nil
		}
	}
	if err != nil {
		if yt.verbose {
			yt.log.Printf("Subtitle download error (%s): %v\n", primarySubLangs, err)
			yt.log.Printf("Command output: %s\n", string(output))
		}

		// Check if this was a rate limit error - if so, don't retry with more variants
		if strings.Contains(string(output), "429") || strings.Contains(string(output), "Too Many Requests") {
			return fmt.Errorf("%w: rate limited", ErrDownloadFailed)
		}

		// Retry with a broader English wildcard when available
		if fallbackSubLangs != "" {
			if yt.verbose && !yt.quiet {
				yt.log.Printf("Trying fallback subtitle languages: %s\n", fallbackSubLangs)
			}
			if err := setSubLangsArg(args, fallbackSubLangs); err != nil {
				return fmt.Errorf("configuring fallback subtitle languages: %w", err)
			}
			output, err = yt.executor.Run(ctx, "yt-dlp", args...)
			attemptedFallback = true
			if err != nil {
				// If subtitles were partially downloaded, allow success
				if files, globErr := filepath.Glob(pattern); globErr == nil && len(files) > 0 {
					if yt.verbose && !yt.quiet {
						yt.log.Printf("Subtitles downloaded despite fallback errors (%s): %v\n", fallbackSubLangs, files)
					}
					err = nil
				}
			}
			if err != nil {
				if yt.verbose {
					yt.log.Printf("Fallback subtitle download error: %v\n", err)
					yt.log.Printf("Command output: %s\n", string(output))
				}
				return fmt.Errorf("%w: %v", ErrDownloadFailed, err)
			}
		} else {
			return fmt.Errorf("%w: %v", ErrDownloadFailed, err)
		}
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Subtitle download completed\n")
	}

	// Check for the downloaded subtitle files
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		// If no files were found and we haven't tried the fallback yet, give it one more attempt
		if len(files) == 0 && fallbackSubLangs != "" && !attemptedFallback {
			if yt.verbose && !yt.quiet {
				yt.log.Printf("No subtitles found for %s, retrying with: %s\n", primarySubLangs, fallbackSubLangs)
			}
			if err := setSubLangsArg(args, fallbackSubLangs); err != nil {
				return fmt.Errorf("configuring fallback subtitle languages: %w", err)
			}
			output, err = yt.executor.Run(ctx, "yt-dlp", args...)
			if err != nil {
				// If subtitles were partially downloaded, allow success
				if files, globErr := filepath.Glob(pattern); globErr == nil && len(files) > 0 {
					if yt.verbose && !yt.quiet {
						yt.log.Printf("Subtitles downloaded despite fallback errors (%s): %v\n", fallbackSubLangs, files)
					}
					err = nil
				}
			}
			if err != nil {
				if yt.verbose {
					yt.log.Printf("Fallback subtitle download error: %v\n", err)
					yt.log.Printf("Command output: %s\n", string(output))
				}
				return fmt.Errorf("%w: %v", ErrDownloadFailed, err)
			}
			files, err = filepath.Glob(pattern)
		}

		if err != nil || len(files) == 0 {
			if yt.verbose {
				yt.log.Printf("No subtitle files found after download\n")
				yt.log.Printf("Searched for pattern: %s\n", pattern)
			}
			return fmt.Errorf("no subtitle files found after download")
		}
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Found %d subtitle file(s): %v\n", len(files), files)
	}

	return nil
}

func (yt *YouTube) fetchStructuredTranscript(ctx context.Context, youtubeURL string, subLangs []string, originalLang string) (*Transcript, error) {
	youtubeID, err := getVideoID(youtubeURL)
	if err != nil {
		return nil, fmt.Errorf("extracting video ID: %w", err)
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Looking for existing transcript for video ID: %s\n", youtubeID)
	}

	// Look for an existing transcript first
	transcriptPath, err := yt.findExistingTranscript(youtubeID)
	if err != nil {
		return nil, fmt.Errorf("error searching for existing transcript: %w", err)
	}

	if transcriptPath != "" {
		if yt.verbose && !yt.quiet {
			yt.log.Printf("Found existing transcript: %s\n", transcriptPath)
		}
		// Process the existing transcript
		return yt.processSrtTranscript(transcriptPath)
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("No existing transcript found, attempting to download...\n")
	}

	// No existing transcript found, try to download one
	err = yt.Transcript(ctx, youtubeURL, subLangs, originalLang)
	if err != nil {
		// Preserve the error type for retry logic
		return nil, err
	}

	// Look for the downloaded transcript
	transcriptPath, err = yt.findExistingTranscript(youtubeID)
	if err != nil || transcriptPath == "" {
		if yt.verbose {
			yt.log.Printf("Could not find downloaded transcript: %v\n", err)
		}
		return nil, fmt.Errorf("downloaded transcript not found")
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Successfully downloaded transcript: %s\n", transcriptPath)
	}

	return yt.processSrtTranscript(transcriptPath)
}

// findExistingTranscript locates a previously downloaded transcript
func (yt *YouTube) findExistingTranscript(videoID string) (string, error) {
	// Look in configured cache directory
	cacheDir := yt.cacheDir
	if fileExists(cacheDir) {
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
	if fileExists(yt.transcriptsDir) {
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

// processSrtTranscript converts SRT to the canonical transcript representation.
func (yt *YouTube) processSrtTranscript(filePath string) (*Transcript, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Processing SRT transcript: %s\n", filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading SRT file: %w", err)
	}

	// Extract video ID from filename
	id := strings.Split(filepath.Base(filePath), ".")[0]
	segments := parseSRT(string(content))
	deduplicatedSegments := condenseSubtitleSegments(segments)
	transcript := &Transcript{
		VideoID:  id,
		Source:   TranscriptSourceCaptions,
		Segments: deduplicatedSegments,
	}

	text, err := transcript.Render(TranscriptRenderFormatPlain)
	if err != nil {
		return nil, err
	}
	transcript.Text = text

	// If the file is in the cache directory, remove it after processing
	cacheDir := yt.cacheDir
	if strings.HasPrefix(filePath, cacheDir) && fileExists(filePath) {
		if err := os.Remove(filePath); err != nil {
			yt.log.Printf("Warning: failed to remove SRT file from cache: %v\n", err)
		}
	}

	return transcript, nil
}

// parseSRT extracts timed transcript segments from SRT format.
func parseSRT(content string) []TranscriptSegment {
	var segments []TranscriptSegment
	var current *TranscriptSegment
	var textParts []string

	flushCurrent := func() {
		if current == nil {
			return
		}

		current.Text = strings.TrimSpace(strings.Join(textParts, " "))
		if current.Text != "" {
			segments = append(segments, *current)
		}

		current = nil
		textParts = nil
	}

	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" {
			continue
		}

		if isSRTSequenceNumber(line) {
			continue
		}

		if strings.Contains(line, "-->") {
			flushCurrent()

			start, end, err := parseSRTTiming(line)
			if err != nil {
				current = nil
				textParts = nil
				continue
			}

			current = &TranscriptSegment{
				Start: start,
				End:   end,
			}
			continue
		}

		if current == nil {
			// Ignore sequence numbers or other non-text lines outside a cue.
			continue
		}

		if cleaned := normalizeSubtitleLine(line); cleaned != "" {
			textParts = append(textParts, cleaned)
		}
	}

	flushCurrent()

	return segments
}

func isSRTSequenceNumber(line string) bool {
	for _, r := range line {
		if r < '0' || r > '9' {
			return false
		}
	}
	return line != ""
}

// normalizeSubtitleLine removes subtitle control tokens and normalizes spacing.
func normalizeSubtitleLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	// ASS/SSA override tags (e.g., {\an8}) and inline escapes can leak into SRT output.
	line = assOverrideTagRegex.ReplaceAllString(line, " ")
	line = strings.ReplaceAll(line, `\h`, " ")
	line = strings.ReplaceAll(line, `\N`, " ")
	line = strings.ReplaceAll(line, `\n`, " ")
	line = htmlTagRegex.ReplaceAllString(line, " ")

	return strings.Join(strings.Fields(line), " ")
}

func parseSRTTiming(line string) (float64, float64, error) {
	parts := strings.Split(line, "-->")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid SRT timing line: %q", line)
	}

	start, err := parseSRTTimestamp(parts[0])
	if err != nil {
		return 0, 0, err
	}

	end, err := parseSRTTimestamp(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if end < start {
		return 0, 0, fmt.Errorf("invalid SRT timing range: end precedes start")
	}

	return start, end, nil
}

func parseSRTTimestamp(value string) (float64, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid SRT timestamp: %q", value)
	}

	hours, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, fmt.Errorf("invalid SRT timestamp hours: %w", err)
	}
	if hours < 0 {
		return 0, fmt.Errorf("invalid SRT timestamp hours: %d", hours)
	}

	minutes, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, fmt.Errorf("invalid SRT timestamp minutes: %w", err)
	}
	if minutes < 0 || minutes > 59 {
		return 0, fmt.Errorf("invalid SRT timestamp minutes: %d", minutes)
	}

	secondsParts := strings.Split(parts[2], ",")
	if len(secondsParts) != 2 {
		return 0, fmt.Errorf("invalid SRT timestamp seconds: %q", value)
	}

	seconds, err := strconv.Atoi(strings.TrimSpace(secondsParts[0]))
	if err != nil {
		return 0, fmt.Errorf("invalid SRT timestamp seconds: %w", err)
	}
	if seconds < 0 || seconds > 59 {
		return 0, fmt.Errorf("invalid SRT timestamp seconds: %d", seconds)
	}

	millisecondText := strings.TrimSpace(secondsParts[1])
	milliseconds, err := strconv.Atoi(millisecondText)
	if err != nil {
		return 0, fmt.Errorf("invalid SRT timestamp milliseconds: %w", err)
	}
	if len(millisecondText) != 3 || milliseconds < 0 || milliseconds > 999 {
		return 0, fmt.Errorf("invalid SRT timestamp milliseconds: %s", millisecondText)
	}

	totalSeconds := float64(hours*3600+minutes*60+seconds) + float64(milliseconds)/1000
	return totalSeconds, nil
}

// condenseSubtitleSegments trims rolling subtitle windows down to newly introduced text.
func condenseSubtitleSegments(segments []TranscriptSegment) []TranscriptSegment {
	result := make([]TranscriptSegment, 0, len(segments))
	prevText := ""

	for _, segment := range segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}

		condensedText := text
		switch {
		case prevText == "":
			// Keep the first segment as-is.
		case text == prevText:
			continue
		case strings.HasPrefix(text, prevText):
			condensedText = strings.TrimSpace(strings.TrimPrefix(text, prevText))
		case strings.HasSuffix(prevText, text):
			continue
		default:
			if overlap := longestSubtitleOverlap(prevText, text); overlap != "" && strings.HasPrefix(text, overlap) {
				condensedText = strings.TrimSpace(strings.TrimPrefix(text, overlap))
			}
		}

		if condensedText == "" {
			prevText = text
			continue
		}

		segment.Text = condensedText
		result = append(result, segment)
		prevText = text
	}

	return result
}

func longestSubtitleOverlap(previous, current string) string {
	prevWords := strings.Fields(previous)
	currentWords := strings.Fields(current)

	maxOverlap := min(len(prevWords), len(currentWords))
	for overlapSize := maxOverlap; overlapSize > 0; overlapSize-- {
		suffix := strings.Join(prevWords[len(prevWords)-overlapSize:], " ")
		prefix := strings.Join(currentWords[:overlapSize], " ")
		if suffix == prefix {
			return prefix
		}
	}

	return ""
}

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
func (yt *YouTube) playlistVideoURLs(ctx context.Context, playlistURL string) (*PlaylistInfo, error) {
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
		playlistURL,
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

// captionsAvailable returns true if either manual or automatic captions exist.
func captionsAvailable(subtitles, autoCaptions map[string]any) bool {
	return len(extractCaptionLanguages(subtitles, autoCaptions)) > 0
}

// extractCaptionLanguages returns a sorted, de-duplicated list of caption languages.
func extractCaptionLanguages(subtitles, autoCaptions map[string]any) []string {
	langs := make(map[string]struct{})

	for lang := range subtitles {
		if lang == "live_chat" {
			continue
		}
		langs[lang] = struct{}{}
	}

	for lang := range autoCaptions {
		if lang == "live_chat" {
			continue
		}
		langs[lang] = struct{}{}
	}

	if len(langs) == 0 {
		return nil
	}

	result := make([]string, 0, len(langs))
	for lang := range langs {
		result = append(result, lang)
	}
	sort.Strings(result)
	return result
}
