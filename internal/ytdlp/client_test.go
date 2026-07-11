package ytdlp

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"path/filepath"
)

type mockCommandRunner struct {
	output []byte
	err    error
}

func (m *mockCommandRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return m.output, m.err
}

func (m *mockCommandRunner) RunStreaming(context.Context, string, []string, func(string)) error {
	return m.err
}

type fakeStreamingRunner struct {
	lines []string
	err   error
	calls int
}

func (r *fakeStreamingRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, r.err
}

func (r *fakeStreamingRunner) RunStreaming(_ context.Context, _ string, _ []string, onLine func(string)) error {
	r.calls++
	for _, line := range r.lines {
		onLine(line)
	}
	return r.err
}

func TestAudioWithProgressUsesStreamingCommandAdapter(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	stream := &fakeStreamingRunner{lines: []string{"[download] 50.0%", "[ExtractAudio]"}}
	yt := NewYouTubeWithCache(t.TempDir(), cacheDir, false, true)
	yt.executor = stream
	progress := &mockProgressBar{}

	_, err := yt.AudioWithProgress(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", progress)
	if err != nil {
		t.Fatalf("AudioWithProgress() error = %v", err)
	}
	if stream.calls != 1 {
		t.Fatalf("streaming command calls = %d, want 1", stream.calls)
	}
	if len(progress.sets) == 0 || progress.sets[0] != 40 {
		t.Fatalf("progress sets = %v, want first value 40", progress.sets)
	}
	if progress.desc != "Converting audio" {
		t.Fatalf("progress description = %q, want %q", progress.desc, "Converting audio")
	}
}

func TestAudioWithProgressReturnsStreamingCommandError(t *testing.T) {
	yt := NewYouTubeWithCache(t.TempDir(), t.TempDir(), false, true)
	yt.executor = &fakeStreamingRunner{err: errors.New("stream failed")}

	_, err := yt.AudioWithProgress(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", &mockProgressBar{})
	if err == nil || !strings.Contains(err.Error(), "stream failed") {
		t.Fatalf("AudioWithProgress() error = %v, want streaming error", err)
	}
}

func TestAudioWithProgressUsesConfiguredCacheDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	yt := NewYouTubeWithCache(t.TempDir(), cacheDir, false, true)
	yt.executor = &mockCommandRunner{}

	got, err := yt.AudioWithProgress(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", nil)
	if err != nil {
		t.Fatalf("AudioWithProgress() error = %v", err)
	}

	want := filepath.Join(cacheDir, "dQw4w9WgXcQ.mp3")
	if got != want {
		t.Fatalf("AudioWithProgress() = %q, want %q", got, want)
	}
}

func TestPlaylistVideoURLsSkipsInvalidVideoIDs(t *testing.T) {
	yt := NewYouTube(t.TempDir(), false, true)
	yt.executor = &mockCommandRunner{output: []byte(`{
		"title":"Playlist",
		"entries":[
			{"id":"dQw4w9WgXcQ","title":"valid"},
			{"id":"../../outside","title":"invalid"}
		]
	}`)}

	info, err := yt.PlaylistVideoURLs(context.Background(), "https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("PlaylistVideoURLs() error = %v", err)
	}
	if len(info.Videos) != 1 {
		t.Fatalf("Videos length = %d, want 1 (%v)", len(info.Videos), info.Videos)
	}
	if info.Videos[0].URL() != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("Videos[0] = %+v", info.Videos[0])
	}
}

func TestMetadataUsesChannelURL(t *testing.T) {
	tests := []struct {
		name           string
		json           string
		wantChannelURL string
	}{
		{
			name:           "channel url",
			json:           `{"title":"Test","channel_url":"https://www.youtube.com/channel/UCLKPca3kwwd-B59HNr-_lvA","uploader_url":"https://www.youtube.com/@aiDotEngineer"}`,
			wantChannelURL: "https://www.youtube.com/channel/UCLKPca3kwwd-B59HNr-_lvA",
		},
		{
			name:           "uploader url fallback",
			json:           `{"title":"Test","channel_url":"","uploader_url":"https://www.youtube.com/@aiDotEngineer"}`,
			wantChannelURL: "https://www.youtube.com/@aiDotEngineer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yt := NewYouTube(t.TempDir(), false, true)
			yt.executor = &mockCommandRunner{output: []byte(tt.json)}

			metadata, err := yt.Metadata(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
			if err != nil {
				t.Fatalf("Metadata() error = %v", err)
			}
			if metadata.ChannelURL != tt.wantChannelURL {
				t.Fatalf("ChannelURL = %q, want %q", metadata.ChannelURL, tt.wantChannelURL)
			}
		})
	}
}

func TestMetadataUsesUploadDateAsPublishedAt(t *testing.T) {
	yt := NewYouTube(t.TempDir(), false, true)
	yt.executor = &mockCommandRunner{output: []byte(`{"title":"Test","upload_date":"20260629"}`)}

	metadata, err := yt.Metadata(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("Metadata() error = %v", err)
	}
	if metadata.PublishedAt != "2026-06-29" {
		t.Fatalf("PublishedAt = %q, want 2026-06-29", metadata.PublishedAt)
	}
}

func TestMetadataFallsBackToUploaderWhenChannelMissing(t *testing.T) {
	tests := []struct {
		name         string
		json         string
		wantChannel  string
		wantCreators []string
	}{
		{
			name:         "channel",
			json:         `{"title":"Test","channel":"Upload Channel","uploader":"AI Engineer","creators":["AI Engineer","Matt Pocock"]}`,
			wantChannel:  "Upload Channel",
			wantCreators: []string{"AI Engineer", "Matt Pocock"},
		},
		{
			name:         "uploader",
			json:         `{"title":"Test","channel":"","uploader":"AI Engineer","creator":"AI Engineer, Matt Pocock","creators":["AI Engineer","Matt Pocock"]}`,
			wantChannel:  "AI Engineer",
			wantCreators: []string{"AI Engineer", "Matt Pocock"},
		},
		{
			name:         "creators do not populate channel",
			json:         `{"title":"Test","channel":" ","uploader":"","creator":"","creators":["AI Engineer","Matt Pocock"]}`,
			wantChannel:  "",
			wantCreators: []string{"AI Engineer", "Matt Pocock"},
		},
		{
			name:         "creator does not populate channel",
			json:         `{"title":"Test","channel":"","uploader":"","creator":"AI Engineer, Matt Pocock"}`,
			wantChannel:  "",
			wantCreators: []string{"AI Engineer, Matt Pocock"},
		},
		{
			name:         "creators string does not populate channel",
			json:         `{"title":"Test","channel":"","uploader":"","creator":"","creators":"AI Engineer, Matt Pocock"}`,
			wantChannel:  "",
			wantCreators: []string{"AI Engineer, Matt Pocock"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yt := NewYouTube(t.TempDir(), false, true)
			yt.executor = &mockCommandRunner{output: []byte(tt.json)}

			metadata, err := yt.Metadata(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
			if err != nil {
				t.Fatalf("Metadata() error = %v", err)
			}
			if metadata.Channel != tt.wantChannel {
				t.Fatalf("Channel = %q, want %q", metadata.Channel, tt.wantChannel)
			}
			if strings.Join(metadata.Creators, "|") != strings.Join(tt.wantCreators, "|") {
				t.Fatalf("Creators = %#v, want %#v", metadata.Creators, tt.wantCreators)
			}
		})
	}
}

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
		{"negative hours", "-1:00:00,000", 0, true},
		{"minutes out of range", "00:60:00,000", 0, true},
		{"seconds out of range", "00:00:60,000", 0, true},
		{"milliseconds out of range", "00:00:00,1000", 0, true},
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
		name         string
		preferred    []string
		originalLang string
		wantPrimary  string
		wantFallback string
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

type mockProgressBar struct {
	desc string
	sets []int
}

func (m *mockProgressBar) Set(current int) {
	m.sets = append(m.sets, current)
}

func (m *mockProgressBar) Describe(description string) {
	m.desc = description
}

func (m *mockProgressBar) Finish() {}

func (m *mockProgressBar) Advance() {}

func TestParseSharedProgressConversionStep(t *testing.T) {
	yt := &YouTube{}

	tests := []struct {
		name         string
		startPercent int
		endPercent   int
		wantLastSet  int
	}{
		{"small range", 0, 5, 5},
		{"zero range", 0, 0, 0},
		{"large range", 0, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := &mockProgressBar{}
			pipe := io.NopCloser(strings.NewReader("[ExtractAudio]\n"))

			done := make(chan struct{})
			go func() {
				yt.parseSharedProgress(pipe, bar, tt.startPercent, tt.endPercent)
				close(done)
			}()

			select {
			case <-done:
				// success
			case <-time.After(2 * time.Second):
				t.Fatal("parseSharedProgress timed out — possible infinite loop")
			}

			if len(bar.sets) == 0 {
				t.Fatal("expected at least one Set call")
			}
			last := bar.sets[len(bar.sets)-1]
			if last != tt.wantLastSet {
				t.Errorf("last Set() = %d, want %d", last, tt.wantLastSet)
			}
		})
	}
}
