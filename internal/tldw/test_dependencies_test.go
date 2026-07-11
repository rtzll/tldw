package tldw_test

import (
	"context"

	"github.com/rtzll/tldw/internal/tldw"
)

const testVideoID = "dQw4w9WgXcQ"

type videoStub struct {
	metadata      *tldw.VideoMetadata
	captions      *tldw.Transcript
	captionsErr   error
	playlist      *tldw.PlaylistInfo
	audioPath     string
	metadataCalls int
	captionCalls  int
	audioCalls    int
}

func (stub *videoStub) FetchMetadata(context.Context, tldw.YouTubeRef) (*tldw.VideoMetadata, error) {
	stub.metadataCalls++
	return stub.metadata, nil
}

func (stub *videoStub) FetchCaptions(context.Context, tldw.YouTubeRef, []string, string) (*tldw.Transcript, error) {
	stub.captionCalls++
	return stub.captions, stub.captionsErr
}

func (stub *videoStub) DownloadAudio(context.Context, tldw.YouTubeRef) (string, error) {
	stub.audioCalls++
	return stub.audioPath, nil
}

func (stub *videoStub) FetchPlaylist(context.Context, tldw.YouTubeRef) (*tldw.PlaylistInfo, error) {
	return stub.playlist, nil
}

type memoryStore struct {
	transcript      *tldw.Transcript
	transcriptErr   error
	metadata        *tldw.VideoMetadata
	transcriptSaves int
	metadataSaves   int
}

func (store *memoryStore) LoadTranscript(videoID string) (*tldw.Transcript, error) {
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
	transcribeCalls  int
	sawDeadline      bool
}

func (stub *aiStub) Transcribe(ctx context.Context, _ string) (string, error) {
	stub.transcribeCalls++
	_, stub.sawDeadline = ctx.Deadline()
	return stub.transcription, stub.transcriptionErr
}

func (stub *aiStub) Summary(context.Context, string) (string, error) {
	return stub.summary, nil
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
