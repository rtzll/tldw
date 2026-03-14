package internal

import (
	"errors"
	"fmt"
	"strings"
)

// TranscriptSource identifies how a transcript was obtained.
type TranscriptSource string

const (
	TranscriptSourceCaptions TranscriptSource = "captions"
	TranscriptSourceWhisper  TranscriptSource = "whisper"
)

// TranscriptRenderFormat controls how transcript content is rendered for output.
type TranscriptRenderFormat string

const (
	TranscriptRenderFormatPlain      TranscriptRenderFormat = "plain"
	TranscriptRenderFormatTimestamps TranscriptRenderFormat = "timestamps"
)

// ErrTranscriptTimestampsUnavailable indicates that a transcript has no timing data.
var ErrTranscriptTimestampsUnavailable = errors.New("timestamps are unavailable for this transcript")

// TranscriptSegment is a single timed transcript entry.
type TranscriptSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end,omitempty"`
	Text  string  `json:"text"`
}

// Transcript is the canonical transcript representation used internally.
type Transcript struct {
	VideoID  string              `json:"video_id,omitempty"`
	Language string              `json:"language,omitempty"`
	Source   TranscriptSource    `json:"source,omitempty"`
	Text     string              `json:"text,omitempty"`
	Segments []TranscriptSegment `json:"segments,omitempty"`
}

// PlainText renders the transcript as plain text.
func (t *Transcript) PlainText() string {
	if t == nil {
		return ""
	}

	if len(t.Segments) > 0 {
		lines := make([]string, 0, len(t.Segments))
		for _, segment := range t.Segments {
			text := strings.TrimSpace(segment.Text)
			if text != "" {
				lines = append(lines, text)
			}
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}

	return strings.TrimSpace(t.Text)
}

// HasTimestamps reports whether the transcript includes timestamped segments.
func (t *Transcript) HasTimestamps() bool {
	return t != nil && len(t.Segments) > 0
}

// Render renders the transcript in the requested format.
func (t *Transcript) Render(format TranscriptRenderFormat) (string, error) {
	switch format {
	case TranscriptRenderFormatPlain:
		text := t.PlainText()
		if text == "" {
			return "", fmt.Errorf("transcript is empty")
		}
		return text, nil
	case TranscriptRenderFormatTimestamps:
		if !t.HasTimestamps() {
			return "", ErrTranscriptTimestampsUnavailable
		}

		lines := make([]string, 0, len(t.Segments))
		for _, segment := range t.Segments {
			text := strings.TrimSpace(segment.Text)
			if text == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("[%s] %s", formatTranscriptTimestamp(segment.Start), text))
		}

		if len(lines) == 0 {
			return "", fmt.Errorf("transcript is empty")
		}

		return strings.Join(lines, "\n"), nil
	default:
		return "", fmt.Errorf("unsupported transcript render format: %s", format)
	}
}

func formatTranscriptTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}

	totalSeconds := int(seconds)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	remainingSeconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, remainingSeconds)
	}

	return fmt.Sprintf("%02d:%02d", minutes, remainingSeconds)
}
