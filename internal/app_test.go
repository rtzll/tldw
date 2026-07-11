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
