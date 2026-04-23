package internal

import (
	"testing"
)

func TestTranscriptPlainText(t *testing.T) {
	tests := []struct {
		name string
		t    *Transcript
		want string
	}{
		{
			name: "nil transcript",
			t:    nil,
			want: "",
		},
		{
			name: "text only",
			t:    &Transcript{Text: "Hello world"},
			want: "Hello world",
		},
		{
			name: "segments only",
			t: &Transcript{
				Segments: []TranscriptSegment{
					{Start: 0, End: 1, Text: "Hello"},
					{Start: 1, End: 2, Text: "world"},
				},
			},
			want: "Hello\nworld",
		},
		{
			name: "segments with empty text",
			t: &Transcript{
				Segments: []TranscriptSegment{
					{Start: 0, End: 1, Text: "Hello"},
					{Start: 1, End: 2, Text: "  "},
					{Start: 2, End: 3, Text: "world"},
				},
			},
			want: "Hello\nworld",
		},
		{
			name: "segments with whitespace",
			t: &Transcript{
				Segments: []TranscriptSegment{
					{Start: 0, End: 1, Text: "  Hello  "},
					{Start: 1, End: 2, Text: "  world  "},
				},
			},
			want: "Hello\nworld",
		},
		{
			name: "both text and segments prefers segments",
			t: &Transcript{
				Text: "ignored text",
				Segments: []TranscriptSegment{
					{Start: 0, End: 1, Text: "segment text"},
				},
			},
			want: "segment text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.t.PlainText(); got != tt.want {
				t.Errorf("Transcript.PlainText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTranscriptHasTimestamps(t *testing.T) {
	tests := []struct {
		name string
		t    *Transcript
		want bool
	}{
		{"nil", nil, false},
		{"no segments", &Transcript{}, false},
		{"has segments", &Transcript{Segments: []TranscriptSegment{{Start: 0}}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.t.HasTimestamps(); got != tt.want {
				t.Errorf("Transcript.HasTimestamps() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTranscriptRender(t *testing.T) {
	tests := []struct {
		name    string
		t       *Transcript
		format  TranscriptRenderFormat
		want    string
		wantErr bool
	}{
		{
			name:    "plain text from segments",
			t:       &Transcript{Segments: []TranscriptSegment{{Start: 0, End: 1, Text: "Hello world"}}},
			format:  TranscriptRenderFormatPlain,
			want:    "Hello world",
			wantErr: false,
		},
		{
			name:    "plain text empty",
			t:       &Transcript{},
			format:  TranscriptRenderFormatPlain,
			want:    "",
			wantErr: true,
		},
		{
			name:    "timestamps available",
			t:       &Transcript{Segments: []TranscriptSegment{{Start: 65, End: 70, Text: "Hello world"}}},
			format:  TranscriptRenderFormatTimestamps,
			want:    "[01:05] Hello world",
			wantErr: false,
		},
		{
			name:    "timestamps unavailable",
			t:       &Transcript{Text: "Hello world"},
			format:  TranscriptRenderFormatTimestamps,
			want:    "",
			wantErr: true,
		},
		{
			name:    "unsupported format",
			t:       &Transcript{Text: "Hello"},
			format:  "invalid",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.t.Render(tt.format)
			if (err != nil) != tt.wantErr {
				t.Errorf("Transcript.Render() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Transcript.Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatTranscriptTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		seconds float64
		want    string
	}{
		{"zero", 0, "00:00"},
		{"seconds only", 45, "00:45"},
		{"one minute", 60, "01:00"},
		{"minutes and seconds", 125, "02:05"},
		{"one hour", 3600, "01:00:00"},
		{"hours minutes seconds", 3661, "01:01:01"},
		{"negative", -5, "00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTranscriptTimestamp(tt.seconds); got != tt.want {
				t.Errorf("formatTranscriptTimestamp(%v) = %q, want %q", tt.seconds, got, tt.want)
			}
		})
	}
}
