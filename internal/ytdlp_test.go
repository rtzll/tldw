package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSRT(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []TranscriptSegment
	}{
		{
			name: "basic SRT",
			content: `1
00:00:01,000 --> 00:00:04,000
Hello world

2
00:00:05,000 --> 00:00:07,000
Second line
`,
			want: []TranscriptSegment{
				{Start: 1, End: 4, Text: "Hello world"},
				{Start: 5, End: 7, Text: "Second line"},
			},
		},
		{
			name: "multiline text",
			content: `1
00:00:01,000 --> 00:00:04,000
First line
Second line

2
00:00:05,000 --> 00:00:07,000
Third line
`,
			want: []TranscriptSegment{
				{Start: 1, End: 4, Text: "First line Second line"},
				{Start: 5, End: 7, Text: "Third line"},
			},
		},
		{
			name:    "empty content",
			content: "",
			want:    nil,
		},
		{
			name: "ASS tags stripped",
			content: `1
00:00:01,000 --> 00:00:04,000
{\an8}Hello world
`,
			want: []TranscriptSegment{
				{Start: 1, End: 4, Text: "Hello world"},
			},
		},
		{
			name: "HTML tags stripped",
			content: `1
00:00:01,000 --> 00:00:04,000
<b>Hello</b> world
`,
			want: []TranscriptSegment{
				{Start: 1, End: 4, Text: "Hello world"},
			},
		},
		{
			name: "invalid timing skipped",
			content: `1
00:00:01,000 --> 00:00:04,000
Valid line
2
invalid timing --> 00:00:06,000
Skipped line
`,
			want: []TranscriptSegment{
				{Start: 1, End: 4, Text: "Valid line"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSRT(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("parseSRT() returned %d segments, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i].Start != tt.want[i].Start || got[i].End != tt.want[i].End || got[i].Text != tt.want[i].Text {
					t.Errorf("parseSRT() segment %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeSubtitleLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"plain text", "Hello world", "Hello world"},
		{"ASS tag", `{\an8}Hello`, "Hello"},
		{"HTML tag", "<b>Hello</b>", "Hello"},
		{"escaped spaces", `Hello\hworld`, "Hello world"},
		{"newline escape", `Hello\Nworld`, "Hello world"},
		{"multiple spaces", "Hello    world", "Hello world"},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSubtitleLine(tt.line); got != tt.want {
				t.Errorf("normalizeSubtitleLine(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestParseSRTTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    float64
		wantErr bool
	}{
		{"zero", "00:00:00,000", 0, false},
		{"seconds", "00:00:45,500", 45.5, false},
		{"minutes", "00:05:00,000", 300, false},
		{"hours", "02:30:15,000", 9015, false},
		{"with spaces", " 00:00:01,000 ", 1, false},
		{"invalid format", "00:00", 0, true},
		{"invalid hours", "ab:00:00,000", 0, true},
		{"invalid minutes", "00:ab:00,000", 0, true},
		{"invalid seconds", "00:00:ab,000", 0, true},
		{"invalid ms", "00:00:00,abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSRTTimestamp(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSRTTimestamp(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseSRTTimestamp(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestIsSRTSequenceNumber(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"number", "123", true},
		{"zero", "0", true},
		{"empty", "", false},
		{"text", "Hello", false},
		{"mixed", "12a", false},
		{"timing", "00:00:01,000 --> 00:00:04,000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSRTSequenceNumber(tt.line); got != tt.want {
				t.Errorf("isSRTSequenceNumber(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestCondenseSubtitleSegments(t *testing.T) {
	tests := []struct {
		name     string
		segments []TranscriptSegment
		want     []TranscriptSegment
	}{
		{
			name: "no overlap",
			segments: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello"},
				{Start: 1, End: 2, Text: "world"},
			},
			want: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello"},
				{Start: 1, End: 2, Text: "world"},
			},
		},
		{
			name: "prefix overlap",
			segments: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello world"},
				{Start: 1, End: 2, Text: "Hello world today"},
			},
			want: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello world"},
				{Start: 1, End: 2, Text: "today"},
			},
		},
		{
			name: "exact duplicate",
			segments: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello"},
				{Start: 1, End: 2, Text: "Hello"},
			},
			want: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello"},
			},
		},
		{
			name: "suffix overlap skipped",
			segments: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello world"},
				{Start: 1, End: 2, Text: "world"},
			},
			want: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello world"},
			},
		},
		{
			name: "empty text skipped",
			segments: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello"},
				{Start: 1, End: 2, Text: ""},
				{Start: 2, End: 3, Text: "world"},
			},
			want: []TranscriptSegment{
				{Start: 0, End: 1, Text: "Hello"},
				{Start: 2, End: 3, Text: "world"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := condenseSubtitleSegments(tt.segments)
			if len(got) != len(tt.want) {
				t.Errorf("condenseSubtitleSegments() returned %d segments, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i].Start != tt.want[i].Start || got[i].End != tt.want[i].End || got[i].Text != tt.want[i].Text {
					t.Errorf("condenseSubtitleSegments() segment %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLongestSubtitleOverlap(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		current  string
		want     string
	}{
		{"no overlap", "Hello world", "Goodbye", ""},
		{"single word overlap", "Hello world", "world today", "world"},
		{"multi word overlap", "The quick brown", "quick brown fox", "quick brown"},
		{"empty previous", "", "Hello", ""},
		{"empty current", "Hello", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := longestSubtitleOverlap(tt.previous, tt.current); got != tt.want {
				t.Errorf("longestSubtitleOverlap(%q, %q) = %q, want %q", tt.previous, tt.current, got, tt.want)
			}
		})
	}
}

func TestBuildSubLangs(t *testing.T) {
	tests := []struct {
		name           string
		preferred      []string
		originalLang   string
		wantPrimary    string
		wantFallback   string
	}{
		{"no preferred", nil, "", "en.*,en", ""},
		{"english preferred", []string{"en-US", "en"}, "", "en-US", "en.*,en"},
		{"non-english preferred", []string{"de"}, "de", "de", "en.*,en"},
		{"multiple non-english", []string{"de", "fr"}, "de", "de", "en.*,en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary, fallback := buildSubLangs(tt.preferred, tt.originalLang)
			if primary != tt.wantPrimary {
				t.Errorf("buildSubLangs() primary = %q, want %q", primary, tt.wantPrimary)
			}
			if fallback != tt.wantFallback {
				t.Errorf("buildSubLangs() fallback = %q, want %q", fallback, tt.wantFallback)
			}
		})
	}
}

func TestPrioritizeCaptionLanguages(t *testing.T) {
	tests := []struct {
		name         string
		preferred    []string
		originalLang string
		want         []string
	}{
		{"empty", nil, "", nil},
		{"english first match", []string{"en-US", "en-GB", "de"}, "", []string{"en-US"}},
		{"original lang", []string{"de", "fr"}, "de", []string{"de"}},
		{"first non-english", []string{"de", "fr"}, "es", []string{"de"}},
		{"dedup", []string{"de", "de", "fr"}, "es", []string{"de"}},
		{"skip live_chat", []string{"live_chat", "de"}, "es", []string{"de"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prioritizeCaptionLanguages(tt.preferred, tt.originalLang)
			if len(got) != len(tt.want) {
				t.Errorf("prioritizeCaptionLanguages() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("prioritizeCaptionLanguages() = %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}

func TestExtractCaptionLanguages(t *testing.T) {
	tests := []struct {
		name         string
		subtitles    map[string]any
		autoCaptions map[string]any
		want         []string
	}{
		{"empty", nil, nil, nil},
		{"manual only", map[string]any{"en": nil, "de": nil}, nil, []string{"de", "en"}},
		{"auto only", nil, map[string]any{"en": nil, "fr": nil}, []string{"en", "fr"}},
		{"combined", map[string]any{"en": nil}, map[string]any{"de": nil}, []string{"de", "en"}},
		{"skip live_chat", map[string]any{"en": nil, "live_chat": nil}, nil, []string{"en"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCaptionLanguages(tt.subtitles, tt.autoCaptions)
			if len(got) != len(tt.want) {
				t.Errorf("extractCaptionLanguages() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractCaptionLanguages() = %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}

func TestCaptionsAvailable(t *testing.T) {
	tests := []struct {
		name         string
		subtitles    map[string]any
		autoCaptions map[string]any
		want         bool
	}{
		{"none", nil, nil, false},
		{"manual", map[string]any{"en": nil}, nil, true},
		{"auto", nil, map[string]any{"en": nil}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := captionsAvailable(tt.subtitles, tt.autoCaptions); got != tt.want {
				t.Errorf("captionsAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetSubLangsArg(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		value   string
		want    []string
		wantErr bool
	}{
		{
			name:    "update existing",
			args:    []string{"--write-subs", "--sub-langs", "en", "--skip-download"},
			value:   "en.*,en",
			want:    []string{"--write-subs", "--sub-langs", "en.*,en", "--skip-download"},
			wantErr: false,
		},
		{
			name:    "flag not found",
			args:    []string{"--write-subs", "--skip-download"},
			value:   "en",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := make([]string, len(tt.args))
			copy(args, tt.args)
			err := setSubLangsArg(args, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setSubLangsArg() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				for i := range args {
					if args[i] != tt.want[i] {
						t.Errorf("setSubLangsArg() args[%d] = %q, want %q", i, args[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestSaveAndLoadStructuredTranscript(t *testing.T) {
	tmpDir := t.TempDir()

	original := &Transcript{
		VideoID:  "dQw4w9WgXcQ",
		Language: "en",
		Source:   TranscriptSourceCaptions,
		Text:     "Hello world",
		Segments: []TranscriptSegment{
			{Start: 0, End: 1, Text: "Hello world"},
		},
	}

	if err := SaveStructuredTranscript(original, tmpDir); err != nil {
		t.Fatalf("SaveStructuredTranscript() error = %v", err)
	}

	loaded, err := LoadStructuredTranscript("dQw4w9WgXcQ", tmpDir)
	if err != nil {
		t.Fatalf("LoadStructuredTranscript() error = %v", err)
	}

	if loaded.VideoID != original.VideoID || loaded.Language != original.Language || loaded.Text != original.Text {
		t.Errorf("LoadStructuredTranscript() = %+v, want %+v", loaded, original)
	}
	if len(loaded.Segments) != len(original.Segments) {
		t.Errorf("LoadStructuredTranscript() segments = %d, want %d", len(loaded.Segments), len(original.Segments))
	}
}

func TestSaveAndLoadMetadata(t *testing.T) {
	tmpDir := t.TempDir()

	original := &VideoMetadata{
		Title:       "Test Video",
		Channel:     "Test Channel",
		Duration:    120,
		Description: "A test video",
		HasCaptions: true,
	}

	if err := SaveMetadata("dQw4w9WgXcQ", original, tmpDir); err != nil {
		t.Fatalf("SaveMetadata() error = %v", err)
	}

	loaded, err := LoadCachedMetadata("dQw4w9WgXcQ", tmpDir)
	if err != nil {
		t.Fatalf("LoadCachedMetadata() error = %v", err)
	}

	if loaded.Title != original.Title || loaded.Channel != original.Channel || loaded.Duration != original.Duration {
		t.Errorf("LoadCachedMetadata() = %+v, want %+v", loaded, original)
	}
}

func TestSaveTranscript(t *testing.T) {
	tmpDir := t.TempDir()

	if err := SaveTranscript("dQw4w9WgXcQ", "Hello world", tmpDir); err != nil {
		t.Fatalf("SaveTranscript() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "dQw4w9WgXcQ.txt"))
	if err != nil {
		t.Fatalf("reading saved transcript: %v", err)
	}

	if string(content) != "Hello world" {
		t.Errorf("saved transcript = %q, want %q", string(content), "Hello world")
	}
}
