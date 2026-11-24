package ytdlp

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rtzll/tldw/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"

	"github.com/rtzll/tldw/internal/tldw"
)

var (
	assOverrideTagRegex = regexp.MustCompile(`\{\\[^}]*\}`)
	htmlTagRegex        = regexp.MustCompile(`<[^>]+>`)
)

func (yt *Client) processSrtTranscript(ctx context.Context, filePath string, track tldw.CaptionTrack) (result *tldw.Transcript, err error) {
	_, span := telemetry.Start(ctx, "captions.parse")
	defer telemetry.EndSpan(span)
	defer func() { tldw.RecordOperationError(span, err) }()
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Processing SRT transcript: %s\n", filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading SRT file: %w", err)
	}

	// Extract video ID from filename
	base := strings.TrimSuffix(filepath.Base(filePath), ".srt")
	id := strings.TrimSuffix(base, filepath.Ext(base))
	segments := parseSRT(string(content))
	if track.Automatic {
		segments = condenseSubtitleSegments(segments)
	} else {
		for i := range segments {
			segments[i].Text = strings.Join(strings.Fields(segments[i].Text), " ")
		}
	}
	span.SetAttributes(attribute.Int("tldw.caption.segments", len(segments)))
	transcript, err := tldw.NewCaptionTranscript(id, track, "", segments)
	if err != nil {
		return nil, err
	}

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

type cuePhase uint8

const (
	outsideCue cuePhase = iota
	awaitingText
	collectingText
)

type cueParser struct {
	phase    cuePhase
	cue      tldw.TranscriptSegment
	lines    []string
	segments []tldw.TranscriptSegment
}

// finish closes the current cue at a separator, another timing line, or EOF.
func (p *cueParser) finish() {
	if p.phase == collectingText {
		p.cue.Text = strings.Join(p.lines, "\n")
		p.segments = append(p.segments, p.cue)
	}
	p.phase = outsideCue
	p.lines = p.lines[:0]
}

func parseSRT(content string) []tldw.TranscriptSegment {
	var p cueParser
	lines := strings.Split(content, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			if p.phase == collectingText {
				p.finish()
			}
		case isSRTSequenceNumber(line) && i+1 < len(lines) && strings.Contains(lines[i+1], "-->"):
			// Digits elsewhere in a cue are spoken text.
		case strings.Contains(line, "-->"):
			p.finish()
			if start, end, err := parseSRTTiming(line); err == nil {
				p.cue = tldw.TranscriptSegment{Start: start, End: end}
				p.phase = awaitingText
			}
		case p.phase != outsideCue:
			if text := normalizeSubtitleLine(line); text != "" {
				p.lines = append(p.lines, text)
				p.phase = collectingText
			}
		}
	}
	p.finish()
	return p.segments
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

	totalSeconds := float64(hours)*3600 + float64(minutes*60+seconds) + float64(milliseconds)/1000
	if totalSeconds >= float64(math.MaxInt) {
		return 0, fmt.Errorf("SRT timestamp exceeds supported range")
	}
	return totalSeconds, nil
}

// condenseSubtitleSegments trims rolling subtitle windows down to newly introduced text.
func condenseSubtitleSegments(segments []tldw.TranscriptSegment) []tldw.TranscriptSegment {
	result := make([]tldw.TranscriptSegment, 0, len(segments))
	var previousRaw tldw.TranscriptSegment

	for i, segment := range segments {
		text := strings.Join(strings.Fields(segment.Text), " ")
		if text == "" {
			continue
		}

		condensedText := text
		connected := subtitleWindowsConnected(previousRaw, segment)
		// Longer retained-line transitions need a complete three-window
		// pattern. Adjacent matching multiline speech alone is insufficient.
		if !connected && i+1 < len(segments) {
			connected = rollingSubtitleBridge(previousRaw, segment, segments[i+1])
		}
		if !connected && i >= 2 {
			connected = rollingSubtitleBridge(segments[i-2], previousRaw, segment)
		}
		if connected {
			if overlap := longestSubtitleOverlap(previousRaw.Text, text); overlap != "" {
				condensedText = strings.TrimSpace(strings.TrimPrefix(text, overlap))
			}
		}

		// Track the raw window even when it contributes no new words. A rolling
		// window can shrink before introducing another occurrence of a word.
		previousRaw = segment
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
	// Converted rolling windows can be contiguous without overlapping in time.
	// Require display-line or short-transition evidence before removing text
	// from ordinary back-to-back speech (e.g. "Yes." then "Yes.").
	// Timing has millisecond precision; epsilon only absorbs float rounding.
	const epsilon = 0.000001
	if current.Start-previous.End > epsilon {
		return false
	}
	previousDuration := previous.End - previous.Start
	currentDuration := current.End - current.Start
	// The first single-line window has no line boundary to compare. Its short
	// transition cue varies in duration; allow up to 50 ms without line evidence.
	const shortTransition = 0.05
	return (previousDuration > 0 && previousDuration <= shortTransition+epsilon) ||
		(currentDuration > 0 && currentDuration <= shortTransition+epsilon)
}

func rollingSubtitleBridge(previous, retained, next tldw.TranscriptSegment) bool {
	const epsilon = 0.000001
	if retained.Start < previous.Start || next.Start < retained.Start ||
		retained.End <= retained.Start ||
		math.Abs(retained.Start-previous.End) > epsilon || math.Abs(next.Start-retained.End) > epsilon {
		return false
	}
	previousLines := strings.Split(strings.TrimSpace(previous.Text), "\n")
	retainedLines := strings.Split(strings.TrimSpace(retained.Text), "\n")
	nextLines := strings.Split(strings.TrimSpace(next.Text), "\n")
	return len(previousLines) > 1 && len(retainedLines) == 1 && len(nextLines) > 1 &&
		retainedLines[0] != "" && previousLines[len(previousLines)-1] == retainedLines[0] &&
		nextLines[0] == retainedLines[0]
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
