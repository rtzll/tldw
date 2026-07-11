package ytdlp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

var (
	assOverrideTagRegex = regexp.MustCompile(`\{\\[^}]*\}`)
	htmlTagRegex        = regexp.MustCompile(`<[^>]+>`)
)

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
