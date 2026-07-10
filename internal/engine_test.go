package internal

import (
	"context"
	"errors"
	"os"
	"testing"
)

type engineVideoAdapter struct {
	metadata        *VideoMetadata
	transcript      *Transcript
	transcriptErr   error
	transcriptCalls int
	audioPath       string
	audioCalls      int
	playlist        *PlaylistInfo
}

type engineAIAdapter struct {
	transcription string
	summary       string
}

func newTestEngine(config *Config, options ...EngineOption) *Engine {
	runner := &DefaultCommandRunner{}
	audio := NewAudio(runner, config.TempDir, config.Verbose)
	defaults := []EngineOption{
		WithVideoAdapter(NewYouTubeWithCache(config.TranscriptsDir, config.CacheDir, config.Verbose, config.Quiet)),
		WithVideoStore(NewFileVideoStore(config.TranscriptsDir)),
		WithAIAdapter(NewAIWithKey(config.OpenAIAPIKey, audio, config.TLDRModel, WhisperLimit, config.SummaryTimeout, config.Verbose, config.Quiet)),
	}
	return NewEngine(config, append(defaults, options...)...)
}

func (f *engineAIAdapter) Transcribe(context.Context, string) (string, error) {
	return f.transcription, nil
}

func (f *engineAIAdapter) Summary(context.Context, string) (string, error) {
	return f.summary, nil
}

func (f *engineVideoAdapter) FetchMetadata(context.Context, YouTubeRef) (*VideoMetadata, error) {
	return f.metadata, nil
}

func (f *engineVideoAdapter) FetchCaptions(context.Context, YouTubeRef, []string, string) (*Transcript, error) {
	f.transcriptCalls++
	if f.transcriptErr != nil {
		return nil, f.transcriptErr
	}
	return f.transcript, nil
}

func (f *engineVideoAdapter) DownloadAudio(context.Context, YouTubeRef) (string, error) {
	f.audioCalls++
	if f.audioPath == "" {
		return "", errors.New("unexpected audio download")
	}
	if err := os.WriteFile(f.audioPath, []byte("audio"), 0o644); err != nil {
		return "", err
	}
	return f.audioPath, nil
}

func TestEngineTranscriptWhisperOnlySkipsCaptionsAndCachesResult(t *testing.T) {
	ref, err := ParseVideoArg("dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ParseVideoArg() error = %v", err)
	}

	tempDir := t.TempDir()
	video := &engineVideoAdapter{
		metadata:   &VideoMetadata{HasCaptions: true, CaptionLanguages: []string{"en"}},
		transcript: &Transcript{Source: TranscriptSourceCaptions, Text: "caption transcript"},
		audioPath:  tempDir + "/audio.mp3",
	}
	audio := NewAudio(&mockCommandRunner{}, tempDir, false)
	ai := NewAI(&mockOpenAIClient{transcription: "whisper transcript"}, audio, "gpt-5.4-mini", WhisperLimit, 0, false, true)
	engine := newTestEngine(
		&Config{TranscriptsDir: tempDir, Quiet: true},
		WithVideoAdapter(video),
		WithAI(ai),
	)
	if err := SaveStructuredTranscript(&Transcript{
		VideoID: "dQw4w9WgXcQ",
		Source:  TranscriptSourceCaptions,
		Text:    "cached caption transcript",
	}, tempDir); err != nil {
		t.Fatalf("SaveStructuredTranscript() error = %v", err)
	}
	got, err := engine.Transcript(context.Background(), ref, TranscriptRequest{Policy: TranscriptPolicyWhisperOnly})
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got.Source != TranscriptSourceWhisper || got.PlainText() != "whisper transcript" {
		t.Fatalf("Transcript() = %+v, want cached Whisper transcript", got)
	}
	if video.transcriptCalls != 0 {
		t.Fatalf("caption transcript calls = %d, want 0", video.transcriptCalls)
	}
	if video.audioCalls != 1 {
		t.Fatalf("audio calls = %d, want 1", video.audioCalls)
	}
}

func TestEngineTranscriptCaptionsOnlyDoesNotUseCachedWhisper(t *testing.T) {
	ref, err := ParseVideoArg("dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ParseVideoArg() error = %v", err)
	}

	tempDir := t.TempDir()
	if err := SaveStructuredTranscript(&Transcript{
		VideoID: "dQw4w9WgXcQ",
		Source:  TranscriptSourceWhisper,
		Text:    "paid transcript",
	}, tempDir); err != nil {
		t.Fatalf("SaveStructuredTranscript() error = %v", err)
	}
	if err := SaveTranscript("dQw4w9WgXcQ", "paid transcript", tempDir); err != nil {
		t.Fatalf("SaveTranscript() error = %v", err)
	}
	engine := newTestEngine(
		&Config{TranscriptsDir: tempDir, Quiet: true},
		WithVideoAdapter(&engineVideoAdapter{metadata: &VideoMetadata{HasCaptions: false}}),
	)

	_, err = engine.Transcript(context.Background(), ref, TranscriptRequest{Policy: TranscriptPolicyCaptionsOnly})
	if !errors.Is(err, ErrCaptionsUnavailable) {
		t.Fatalf("Transcript() error = %v, want ErrCaptionsUnavailable", err)
	}
}

func (f *engineVideoAdapter) FetchPlaylist(context.Context, YouTubeRef) (*PlaylistInfo, error) {
	if f.playlist == nil {
		return nil, errors.New("unexpected playlist lookup")
	}
	return f.playlist, nil
}

func TestEngineTranscriptCachesCaptionResult(t *testing.T) {
	ref, err := ParseVideoArg("dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ParseVideoArg() error = %v", err)
	}

	video := &engineVideoAdapter{
		metadata: &VideoMetadata{
			HasCaptions:      true,
			CaptionLanguages: []string{"en"},
		},
		transcript: &Transcript{
			Source:   TranscriptSourceCaptions,
			Segments: []TranscriptSegment{{Start: 0, End: 2, Text: "hello world"}},
		},
	}
	engine := newTestEngine(&Config{TranscriptsDir: t.TempDir(), Quiet: true}, WithVideoAdapter(video))

	first, err := engine.Transcript(context.Background(), ref, TranscriptRequest{Policy: TranscriptPolicyCaptionsOnly})
	if err != nil {
		t.Fatalf("Transcript() error = %v", err)
	}
	if got := first.PlainText(); got != "hello world" {
		t.Fatalf("Transcript().PlainText() = %q, want %q", got, "hello world")
	}

	video.transcriptErr = errors.New("source should not be called after caching")
	second, err := engine.Transcript(context.Background(), ref, TranscriptRequest{Policy: TranscriptPolicyCaptionsOnly})
	if err != nil {
		t.Fatalf("cached Transcript() error = %v", err)
	}
	if got := second.PlainText(); got != "hello world" {
		t.Fatalf("cached Transcript().PlainText() = %q, want %q", got, "hello world")
	}
	if video.transcriptCalls != 1 {
		t.Fatalf("video transcript calls = %d, want 1", video.transcriptCalls)
	}
}

func TestEngineSummarizeVideoReturnsUnrenderedMarkdown(t *testing.T) {
	ref, err := ParseVideoArg("dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ParseVideoArg() error = %v", err)
	}

	video := &engineVideoAdapter{
		metadata:   &VideoMetadata{Title: "Example", HasCaptions: true, CaptionLanguages: []string{"en"}},
		transcript: &Transcript{Source: TranscriptSourceCaptions, Text: "source transcript"},
	}
	engine := newTestEngine(
		&Config{TranscriptsDir: t.TempDir(), Prompt: "Summarize: {{.Transcript}}", Quiet: true},
		WithVideoAdapter(video),
		WithAIAdapter(&engineAIAdapter{summary: "## Raw summary"}),
	)

	summary, err := engine.SummarizeVideo(context.Background(), ref, TranscriptRequest{Policy: TranscriptPolicyCaptionsOnly})
	if err != nil {
		t.Fatalf("SummarizeVideo() error = %v", err)
	}
	if summary.Markdown != "## Raw summary" {
		t.Fatalf("SummarizeVideo().Markdown = %q, want unrendered Markdown", summary.Markdown)
	}
}

func TestEngineSummarizePlaylistReturnsResultWithoutPrinting(t *testing.T) {
	ref, err := ParseYouTubeArg("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseYouTubeArg() error = %v", err)
	}

	video := &engineVideoAdapter{
		metadata:   &VideoMetadata{Title: "First", Channel: "Channel", HasCaptions: true, CaptionLanguages: []string{"en"}},
		transcript: &Transcript{Source: TranscriptSourceCaptions, Text: "playlist transcript"},
		playlist: &PlaylistInfo{
			Title:  "Examples",
			Videos: []YouTubeRef{{ContentType: ContentTypeVideo, NormalizedURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", ID: "dQw4w9WgXcQ"}},
		},
	}
	engine := newTestEngine(
		&Config{TranscriptsDir: t.TempDir(), Prompt: "Summarize: {{.Transcript}}", Quiet: true},
		WithVideoAdapter(video),
		WithAIAdapter(&engineAIAdapter{summary: "## Playlist summary"}),
	)

	result, err := engine.CreatePlaylistSummary(context.Background(), ref, PlaylistSummaryRequest{
		Transcript: TranscriptRequest{Policy: TranscriptPolicyCaptionsOnly},
	})
	if err != nil {
		t.Fatalf("CreatePlaylistSummary() error = %v", err)
	}
	if result.Markdown != "## Playlist summary" || result.Processed != 1 || result.Total != 1 {
		t.Fatalf("CreatePlaylistSummary() = %+v", result)
	}
}
