// Package tldw contains the transport-neutral domain model and application workflows.
package tldw

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// TranscriptSource identifies how a transcript was obtained.
type TranscriptSource string

const (
	TranscriptSourceCaptions          TranscriptSource = "captions"
	TranscriptSourcePaidTranscription TranscriptSource = "paid_transcription"
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

// Transcript is the canonical transcript representation.
type Transcript struct {
	VideoID  string
	Language string
	source   TranscriptSource
	text     string
	segments []TranscriptSegment
	track    *CaptionTrack
}

// NewCaptionTranscript records the verified track, including for plain-text captions.
func NewCaptionTranscript(id string, track CaptionTrack, text string, segments []TranscriptSegment) (*Transcript, error) {
	if !track.Direct || track.Language == "" {
		return nil, fmt.Errorf("captions require a verified direct track")
	}
	t := &Transcript{VideoID: id, Language: strings.TrimSuffix(track.Language, "-orig"), source: TranscriptSourceCaptions, text: text, segments: slices.Clone(segments), track: &track}
	return t.validateContent()
}

func NewPaidTranscript(id, text string) (*Transcript, error) {
	return (&Transcript{VideoID: id, source: TranscriptSourcePaidTranscription, text: strings.TrimSpace(text)}).validateContent()
}

// NewLegacyTranscript preserves content whose origin is unknown, including
// timing from early structured cache records.
func NewLegacyTranscript(id, text string, segments []TranscriptSegment) (*Transcript, error) {
	return (&Transcript{VideoID: id, text: text, segments: slices.Clone(segments)}).validateContent()
}

func (t *Transcript) validateContent() (*Transcript, error) {
	if t.PlainText() == "" {
		return nil, fmt.Errorf("transcript is empty")
	}
	return t, nil
}

func (t *Transcript) Source() TranscriptSource      { return t.source }
func (t *Transcript) Segments() []TranscriptSegment { return slices.Clone(t.segments) }
func (t *Transcript) CaptionTrack() *CaptionTrack {
	if t.track == nil {
		return nil
	}
	track := *t.track
	return &track
}

// PlainText renders the transcript as plain text.
func (t *Transcript) PlainText() string {
	if t == nil {
		return ""
	}
	if len(t.segments) > 0 {
		lines := make([]string, 0, len(t.segments))
		for _, segment := range t.segments {
			text := strings.TrimSpace(segment.Text)
			if text != "" {
				lines = append(lines, text)
			}
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return strings.TrimSpace(t.text)
}

// HasTimestamps reports whether the transcript includes timestamped segments.
func (t *Transcript) HasTimestamps() bool {
	return t != nil && len(t.segments) > 0
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
		lines := make([]string, 0, len(t.segments))
		for _, segment := range t.segments {
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
