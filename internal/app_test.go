package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestTranscribeAudioAppliesWhisperTimeout(t *testing.T) {
	client := &deadlineOpenAIClient{}
	tempDir := t.TempDir()
	audio := NewAudio(&mockCommandRunner{}, tempDir, false)
	ai := NewAI(client, audio, "gpt-5.4-mini", WhisperLimit, time.Hour, false, true)
	ref, err := ParseVideoArg("dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ParseVideoArg() error = %v", err)
	}
	engine := newTestEngine(
		&Config{WhisperTimeout: time.Minute, TranscriptsDir: tempDir, Quiet: true},
		WithAI(ai),
		WithVideoAdapter(&engineVideoAdapter{audioPath: filepath.Join(tempDir, "audio.mp3")}),
	)

	if _, err := engine.Transcript(context.Background(), ref, TranscriptRequest{Policy: TranscriptPolicyWhisperOnly}); err != nil {
		t.Fatalf("Transcript() error = %v", err)
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

	engine := newTestEngine(&Config{TranscriptsDir: transcriptsDir, Quiet: true})
	got, err := engine.Transcript(context.Background(), YouTubeRef{ContentType: ContentTypeVideo, ID: "../outside"}, TranscriptRequest{Policy: TranscriptPolicyCaptionsOnly})
	if err == nil {
		t.Fatal("expected invalid input error")
	}
	if got != nil && got.PlainText() == "secret" {
		t.Fatal("read transcript outside configured directory")
	}
}

func TestVideoOnlyPathsRejectPlaylists(t *testing.T) {
	engine := newTestEngine(&Config{TranscriptsDir: t.TempDir(), Quiet: true})
	playlist, err := ParseYouTubeArg("https://www.youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseYouTubeArg() error = %v", err)
	}
	if _, err := engine.Transcript(context.Background(), playlist, TranscriptRequest{Policy: TranscriptPolicyCaptionsOnly}); err == nil {
		t.Fatal("expected playlist transcript request to fail")
	}
	if _, err := engine.MetadataFor(context.Background(), playlist); err == nil {
		t.Fatal("expected playlist metadata request to fail")
	}
}

func TestAppCachedMetadata(t *testing.T) {
	app := newTestEngine(&Config{})
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

func TestMetadataRefreshReasonUsesSchemaForOldCacheVersion(t *testing.T) {
	reason := metadataRefreshReason(&VideoMetadata{Channel: "Test Channel", CacheVersion: currentMetadataCacheVersion - 1})
	if reason != "metadata schema" {
		t.Fatalf("metadataRefreshReason() = %q, want metadata schema", reason)
	}
}

func TestMetadataRefreshesCachedMetadataWithMissingChannel(t *testing.T) {
	transcriptsDir := t.TempDir()
	youtube := NewYouTubeWithCache(transcriptsDir, t.TempDir(), false, true)
	youtube.executor = &mockCommandRunner{output: []byte(`{"title":"Fresh Video","channel":"","uploader":"AI Engineer","creators":["AI Engineer","Matt Pocock"]}`)}
	app := newTestEngine(&Config{TranscriptsDir: transcriptsDir, Quiet: true}, WithYouTube(youtube))

	videoID := "dQw4w9WgXcQ"
	if err := SaveMetadata(videoID, &VideoMetadata{Title: "Cached Video", HasCaptions: true, CaptionLanguages: []string{"en"}}, transcriptsDir); err != nil {
		t.Fatalf("SaveMetadata() error = %v", err)
	}

	ref, err := ParseVideoArg("https://www.youtube.com/watch?v=" + videoID)
	if err != nil {
		t.Fatalf("ParseVideoArg() error = %v", err)
	}
	metadata, err := app.MetadataFor(context.Background(), ref)
	if err != nil {
		t.Fatalf("Metadata() error = %v", err)
	}
	if metadata.Title != "Fresh Video" {
		t.Fatalf("Title = %q, want refreshed metadata", metadata.Title)
	}
	if metadata.Channel != "AI Engineer" {
		t.Fatalf("Channel = %q, want fallback uploader", metadata.Channel)
	}
	if strings.Join(metadata.Creators, "|") != "AI Engineer|Matt Pocock" {
		t.Fatalf("Creators = %#v, want both associated creators", metadata.Creators)
	}

	cached, err := LoadCachedMetadata(videoID, transcriptsDir)
	if err != nil {
		t.Fatalf("LoadCachedMetadata() error = %v", err)
	}
	if cached.Channel != "AI Engineer" {
		t.Fatalf("cached Channel = %q, want refreshed channel", cached.Channel)
	}
	if strings.Join(cached.Creators, "|") != "AI Engineer|Matt Pocock" {
		t.Fatalf("cached Creators = %#v, want both associated creators", cached.Creators)
	}
}

func TestBuildPlaylistTranscript(t *testing.T) {
	app := newTestEngine(&Config{})

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
