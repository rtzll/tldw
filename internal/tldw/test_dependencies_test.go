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
	audioPath     string
	metadataCalls int
	captionCalls  int
	audioCalls    int
}

func (stub *videoStub) FetchMetadata(context.Context, tldw.ContentRef) (*tldw.VideoMetadata, error) {
	stub.metadataCalls++
	return stub.metadata, stub.metadataErr
}

func (stub *videoStub) FetchCaptions(context.Context, tldw.ContentRef, *tldw.VideoMetadata) (*tldw.Transcript, error) {
	stub.captionCalls++
	return stub.captions, stub.captionsErr
}

func (stub *videoStub) DownloadAudio(context.Context, tldw.ContentRef) (string, error) {
	stub.audioCalls++
	return stub.audioPath, nil
}

func (stub *videoStub) FetchPlaylist(context.Context, tldw.ContentRef) (*tldw.PlaylistInfo, error) {
	return stub.playlist, nil
}

type memoryStore struct {
	transcripts     map[tldw.TranscriptSource]*tldw.Transcript
	transcriptErr   error
	metadata        *tldw.VideoMetadata
	metadataEntries []tldw.StoredVideoMetadata
	transcriptSaves int
	metadataSaves   int
}

func (store *memoryStore) ListMetadata() (tldw.MetadataScan, error) {
	return tldw.MetadataScan{Entries: store.metadataEntries}, nil
}

func (store *memoryStore) LoadTranscript(_ string, source tldw.TranscriptSource) (*tldw.Transcript, error) {
	if store.transcriptErr != nil {
		return nil, store.transcriptErr
	}
	transcript := store.transcripts[source]
	if transcript == nil {
		return nil, tldw.ErrStoreNotFound
	}
	return transcript, nil
}

func (store *memoryStore) SaveTranscript(transcript *tldw.Transcript) error {
	store.transcriptSaves++
	if store.transcripts == nil {
		store.transcripts = make(map[tldw.TranscriptSource]*tldw.Transcript)
	}
	store.transcripts[transcript.Source()] = transcript
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
	transcription   string
	summary         string
	transcribeCalls int
	sawDeadline     bool
}

func (stub *aiStub) Transcribe(ctx context.Context, _ string) (string, error) {
	stub.transcribeCalls++
	_, stub.sawDeadline = ctx.Deadline()
	return stub.transcription, nil
}

func (stub *aiStub) Summary(context.Context, string) (string, error) {
	return stub.summary, nil
}

type promptStub struct {
	prompt     string
	transcript string
}

func (stub *promptStub) CreatePrompt(transcript string, _ *tldw.VideoMetadata) (string, error) {
	stub.transcript = transcript
	return stub.prompt, nil
}

func fixtureCaptions(t *testing.T, text string, segments ...tldw.TranscriptSegment) *tldw.Transcript {
	t.Helper()
	result, err := tldw.NewCaptionTranscript(testVideoID, tldw.CaptionTrack{Language: "en", Direct: true}, text, segments)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
