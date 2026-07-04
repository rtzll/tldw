package internal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type deadlineOpenAIClient struct {
	sawDeadline bool
}

func (c *deadlineOpenAIClient) CreateTranscription(ctx context.Context, file *os.File) (string, error) {
	_, c.sawDeadline = ctx.Deadline()
	return "hello", nil
}

func (c *deadlineOpenAIClient) CreateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
	return "", nil
}

func TestAppShouldShowStatus(t *testing.T) {
	tests := []struct {
		name    string
		quiet   bool
		verbose bool
		want    bool
	}{
		{"normal", false, false, true},
		{"quiet", true, false, false},
		{"verbose", false, true, false},
		{"both", true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{config: &Config{Quiet: tt.quiet, Verbose: tt.verbose}}
			if got := app.shouldShowStatus(); got != tt.want {
				t.Errorf("shouldShowStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTranscribeAudioAppliesWhisperTimeout(t *testing.T) {
	client := &deadlineOpenAIClient{}
	audio := NewAudio(&mockCommandRunner{}, t.TempDir(), false)
	ai := NewAI(client, audio, "gpt-5.4-mini", WhisperLimit, time.Hour, false, true)
	app := NewApp(&Config{WhisperTimeout: time.Minute, Quiet: true}, WithAI(ai))

	audioFile := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(audioFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to create audio file: %v", err)
	}

	if _, err := app.TranscribeAudioStructured(context.Background(), audioFile); err != nil {
		t.Fatalf("TranscribeAudioStructured() error = %v", err)
	}
	if !client.sawDeadline {
		t.Fatal("expected Whisper transcription context to have a deadline")
	}
}

func TestGetTranscriptRejectsInvalidInputBeforeCacheLookup(t *testing.T) {
	baseDir := t.TempDir()
	transcriptsDir := filepath.Join(baseDir, "transcripts")
	if err := os.Mkdir(transcriptsDir, 0755); err != nil {
		t.Fatalf("failed to create transcripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "outside.txt"), []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to create outside transcript: %v", err)
	}

	app := NewApp(&Config{TranscriptsDir: transcriptsDir, Quiet: true})
	got, err := app.GetTranscriptOutput(context.Background(), "../outside", TranscriptRenderFormatPlain)
	if err == nil {
		t.Fatal("expected invalid input error")
	}
	if got == "secret" {
		t.Fatal("read transcript outside configured directory")
	}
}

func TestVideoOnlyPathsRejectPlaylists(t *testing.T) {
	app := NewApp(&Config{TranscriptsDir: t.TempDir(), Quiet: true})
	playlistURL := "https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"

	if _, err := app.GetTranscriptOutput(context.Background(), playlistURL, TranscriptRenderFormatPlain); err == nil {
		t.Fatal("expected playlist transcript request to fail")
	}
	if _, err := app.Metadata(context.Background(), playlistURL); err == nil {
		t.Fatal("expected playlist metadata request to fail")
	}
}

func TestAppCachedMetadata(t *testing.T) {
	app := NewApp(&Config{})
	meta := &VideoMetadata{Title: "Test"}

	app.setCachedMetadata("abc123", meta)

	got, ok := app.getCachedMetadata("abc123")
	if !ok {
		t.Fatal("expected cached metadata to be found")
	}
	if got.Title != "Test" {
		t.Errorf("getCachedMetadata() Title = %q, want %q", got.Title, "Test")
	}

	_, ok = app.getCachedMetadata("notfound")
	if ok {
		t.Error("expected no metadata for unknown key")
	}
}

func TestCollectPlaylistTranscriptsUsesCachedData(t *testing.T) {
	transcriptsDir := t.TempDir()
	app := NewApp(&Config{TranscriptsDir: transcriptsDir, Quiet: true})

	videoID := "dQw4w9WgXcQ"
	if err := SaveTranscript(videoID, "cached transcript", transcriptsDir); err != nil {
		t.Fatalf("SaveTranscript() error = %v", err)
	}
	metadata := &VideoMetadata{Title: "Cached Video", Channel: "Channel", Duration: 42, Description: "Description"}
	if err := SaveMetadata(videoID, metadata, transcriptsDir); err != nil {
		t.Fatalf("SaveMetadata() error = %v", err)
	}

	info := &PlaylistInfo{
		Title:     "Playlist",
		VideoURLs: []string{"https://www.youtube.com/watch?v=" + videoID},
	}
	videos, skipped := app.collectPlaylistTranscripts(context.Background(), info, false)
	if len(skipped) != 0 {
		t.Fatalf("skipped videos = %v, want none", skipped)
	}
	if len(videos) != 1 {
		t.Fatalf("videos length = %d, want 1", len(videos))
	}
	if videos[0].Title != metadata.Title || videos[0].Transcript != "cached transcript" {
		t.Fatalf("collected video = %+v", videos[0])
	}
}

func TestBuildPlaylistTranscript(t *testing.T) {
	app := NewApp(&Config{})

	videos := []VideoTranscript{
		{
			URL:         "https://youtube.com/watch?v=abc123",
			Title:       "First Video",
			Channel:     "Channel A",
			Duration:    125,
			Description: "Description A",
			Transcript:  "Transcript A",
		},
		{
			URL:         "https://youtube.com/watch?v=def456",
			Title:       "Second Video",
			Channel:     "Channel B",
			Duration:    60,
			Description: "Description B",
			Transcript:  "Transcript B",
		},
	}

	got := app.buildPlaylistTranscript("My Playlist", videos)

	// Check that key elements are present
	expectedParts := []string{
		"Playlist: My Playlist",
		"Video 1 of 2: First Video",
		"Duration: 2:05 | Channel: Channel A",
		"Description: Description A",
		"Transcript A",
		"---",
		"Video 2 of 2: Second Video",
		"Duration: 1:00 | Channel: Channel B",
		"Transcript B",
	}

	for _, part := range expectedParts {
		if !containsSubstr(got, part) {
			t.Errorf("buildPlaylistTranscript() missing expected part: %q\ngot:\n%s", part, got)
		}
	}
}

func containsSubstr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstrHelper(s, substr))
}

func containsSubstrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
