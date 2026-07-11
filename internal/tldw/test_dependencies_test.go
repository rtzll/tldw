package tldw_test

import (
	"context"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

const testVideoID = "dQw4w9WgXcQ"

type videoStub struct {
	metadata      *tldw.VideoMetadata
	metadataErr   error
	captions      *tldw.Transcript
	captionsErr   error
	playlist      *tldw.PlaylistInfo
	playlistErr   error
	audioPath     string
	audioErr      error
	metadataCalls int
	captionCalls  int
	audioCalls    int
	playlistCalls int
}

func (stub *videoStub) FetchMetadata(context.Context, tldw.YouTubeRef) (*tldw.VideoMetadata, error) {
	stub.metadataCalls++
	return stub.metadata, stub.metadataErr
}

func (stub *videoStub) FetchCaptions(context.Context, tldw.YouTubeRef, []string, string) (*tldw.Transcript, error) {
	stub.captionCalls++
	return stub.captions, stub.captionsErr
}

func (stub *videoStub) DownloadAudio(context.Context, tldw.YouTubeRef) (string, error) {
	stub.audioCalls++
	return stub.audioPath, stub.audioErr
}

func (stub *videoStub) FetchPlaylist(context.Context, tldw.YouTubeRef) (*tldw.PlaylistInfo, error) {
	stub.playlistCalls++
	return stub.playlist, stub.playlistErr
}

type memoryStore struct {
	transcript       *tldw.Transcript
	transcriptErr    error
	metadata         *tldw.VideoMetadata
	metadataErr      error
	transcriptSaves  int
	metadataSaves    int
	loadTranscriptID string
	loadMetadataID   string
}

func (store *memoryStore) LoadTranscript(videoID string) (*tldw.Transcript, error) {
	store.loadTranscriptID = videoID
	if store.transcriptErr != nil {
		return nil, store.transcriptErr
	}
	if store.transcript == nil {
		return nil, tldw.ErrStoreNotFound
	}
	return store.transcript, nil
}

func (store *memoryStore) SaveTranscript(transcript *tldw.Transcript) error {
	store.transcriptSaves++
	store.transcript = transcript
	return nil
}

func (store *memoryStore) LoadMetadata(videoID string) (*tldw.VideoMetadata, error) {
	store.loadMetadataID = videoID
	if store.metadataErr != nil {
		return nil, store.metadataErr
	}
	if store.metadata == nil {
		return nil, tldw.ErrStoreNotFound
	}
	return store.metadata, nil
}

func (store *memoryStore) SaveMetadata(_ string, metadata *tldw.VideoMetadata) error {
	store.metadataSaves++
	store.metadata = metadata
	return nil
}

type aiStub struct {
	transcription    string
	transcriptionErr error
	summary          string
	summaryErr       error
	transcribeCalls  int
	summaryCalls     int
	sawDeadline      bool
}

func (stub *aiStub) Transcribe(ctx context.Context, _ string) (string, error) {
	stub.transcribeCalls++
	_, stub.sawDeadline = ctx.Deadline()
	return stub.transcription, stub.transcriptionErr
}

func (stub *aiStub) Summary(context.Context, string) (string, error) {
	stub.summaryCalls++
	return stub.summary, stub.summaryErr
}

type promptStub struct {
	prompt     string
	err        error
	transcript string
	metadata   *tldw.VideoMetadata
}

func (stub *promptStub) CreatePrompt(transcript string, metadata *tldw.VideoMetadata) (string, error) {
	stub.transcript = transcript
	stub.metadata = metadata
	return stub.prompt, stub.err
}

type engineFixture struct {
	engine  *tldw.Engine
	video   *videoStub
	store   *memoryStore
	ai      *aiStub
	prompts *promptStub
}

func newEngineFixture(t *testing.T, config tldw.Config) *engineFixture {
	t.Helper()
	fixture := &engineFixture{
		video: &videoStub{
			metadata:  &tldw.VideoMetadata{HasCaptions: true, CaptionLanguages: []string{"en"}},
			captions:  &tldw.Transcript{Source: tldw.TranscriptSourceCaptions, Text: "caption transcript"},
			audioPath: "audio.mp3",
		},
		store:   &memoryStore{},
		ai:      &aiStub{transcription: "whisper transcript", summary: "summary"},
		prompts: &promptStub{prompt: "prompt"},
	}
	engine, err := tldw.NewEngine(config, tldw.Dependencies{
		Video: fixture.video, Store: fixture.store, AI: fixture.ai, Prompts: fixture.prompts,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	fixture.engine = engine
	return fixture
}

func videoRef(t *testing.T) tldw.YouTubeRef {
	t.Helper()
	ref, err := tldw.ParseVideoRef(testVideoID)
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}
	return ref
}

func playlistRef(t *testing.T) tldw.YouTubeRef {
	t.Helper()
	ref, err := tldw.ParseReference("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}
	return ref
}
