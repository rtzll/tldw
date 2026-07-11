package ytdlp

import (
	"context"
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

// downloadCaptions fetches subtitles using yt-dlp.
// preferredLangs allows us to target known caption languages (from metadata) instead of hardcoding English.
// originalLang is the video's declared language; when English captions are absent we prefer this.
func (yt *YouTube) downloadCaptions(ctx context.Context, ref tldw.YouTubeRef, preferredLangs []string, originalLang string) error {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Downloading subtitles...\n")
	}

	// Create path in configured cache directory
	cacheDir := yt.cacheDir
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	// Set output path in cache directory
	outputPath := filepath.Join(cacheDir, "%(id)s")
	pattern := filepath.Join(cacheDir, fmt.Sprintf("%s*.srt", ref.ID()))

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
		ref.URL(),
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
			return fmt.Errorf("%w: rate limited", tldw.ErrDownloadFailed)
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
				return fmt.Errorf("%w: %v", tldw.ErrDownloadFailed, err)
			}
		} else {
			return fmt.Errorf("%w: %v", tldw.ErrDownloadFailed, err)
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
				return fmt.Errorf("%w: %v", tldw.ErrDownloadFailed, err)
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

func (yt *YouTube) fetchStructuredTranscript(ctx context.Context, ref tldw.YouTubeRef, subLangs []string, originalLang string) (*tldw.Transcript, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Looking for existing transcript for video ID: %s\n", ref.ID())
	}

	// Look for an existing transcript first
	transcriptPath, err := yt.findExistingTranscript(ref.ID())
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
	err = yt.downloadCaptions(ctx, ref, subLangs, originalLang)
	if err != nil {
		// Preserve the error type for retry logic
		return nil, err
	}

	// Look for the downloaded transcript
	transcriptPath, err = yt.findExistingTranscript(ref.ID())
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
func (yt *YouTube) processSrtTranscript(filePath string) (*tldw.Transcript, error) {
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
	transcript := &tldw.Transcript{
		VideoID:  id,
		Source:   tldw.TranscriptSourceCaptions,
		Segments: deduplicatedSegments,
	}

	text, err := transcript.Render(tldw.TranscriptRenderFormatPlain)
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
func parseSRT(content string) []tldw.TranscriptSegment {
	var segments []tldw.TranscriptSegment
	var current *tldw.TranscriptSegment
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

			current = &tldw.TranscriptSegment{
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
func condenseSubtitleSegments(segments []tldw.TranscriptSegment) []tldw.TranscriptSegment {
	result := make([]tldw.TranscriptSegment, 0, len(segments))
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
