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
	id, _, _ := strings.Cut(filepath.Base(filePath), ".")
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

	// Remove downloaded cache files after processing, but retain persistent transcripts.
	if pathWithinDirectory(filePath, yt.cacheDir) {
		if err := os.Remove(filePath); err != nil {
			yt.log.Printf("Warning: failed to remove SRT file from cache: %v\n", err)
		}
	}

	return transcript, nil
}

func pathWithinDirectory(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false
	}
	return relative != "." && filepath.IsLocal(relative)
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

	lines := strings.Split(content, "\n")
	for i, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" {
			// Converted rolling captions can start with an empty display line.
			// Only treat a blank as a separator after collecting actual text.
			if len(textParts) > 0 {
				flushCurrent()
			}
			continue
		}

		// A sequence number precedes a timing line; digits inside a cue are speech.
		if isSRTSequenceNumber(line) && i+1 < len(lines) && strings.Contains(lines[i+1], "-->") {
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
	startText, endText, found := strings.Cut(line, "-->")
	if !found || strings.Contains(endText, "-->") {
		return 0, 0, fmt.Errorf("invalid SRT timing line: %q", line)
	}

	start, err := parseSRTTimestamp(startText)
	if err != nil {
		return 0, 0, err
	}

	end, err := parseSRTTimestamp(endText)
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
	var previous tldw.TranscriptSegment

	for _, segment := range segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}

		condensedText := text
		if subtitleWindowsConnected(previous, segment) {
			if overlap := longestSubtitleOverlap(previous.Text, text); overlap != "" {
				condensedText = strings.TrimSpace(strings.TrimPrefix(text, overlap))
			}
		}

		// Track the raw window even when it contributes no new words. A rolling
		// window can shrink before introducing another occurrence of a word.
		previous = segment
		if condensedText == "" {
			continue
		}

		segment.Text = condensedText
		result = append(result, segment)
	}

	return result
}

func subtitleWindowsConnected(previous, current tldw.TranscriptSegment) bool {
	if current.Start < previous.Start {
		return false
	}
	if current.Start < previous.End {
		return true
	}
	// yt-dlp's YouTube SRT conversion inserts 10 ms transition cues between
	// adjacent rolling windows. Allow overlap removal across those transitions,
	// while keeping ordinary back-to-back speech (e.g. "Yes." then "Yes.").
	// Timing has millisecond precision; epsilon only absorbs float rounding.
	const epsilon = 0.000001
	if current.Start-previous.End > epsilon {
		return false
	}
	previousDuration := previous.End - previous.Start
	currentDuration := current.End - current.Start
	return (previousDuration > 0 && previousDuration <= 0.01+epsilon) ||
		(currentDuration > 0 && currentDuration <= 0.01+epsilon)
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
