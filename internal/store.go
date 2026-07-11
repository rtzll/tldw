package internal

import "github.com/rtzll/tldw/internal/store"

// VideoStore is the local persistence seam used by the application module.
// The filesystem adapter preserves the existing on-disk formats.
type VideoStore interface {
	LoadTranscript(videoID string) (*Transcript, error)
	LoadPlainTranscript(videoID string) (string, error)
	SaveTranscript(transcript *Transcript) error
	LoadMetadata(videoID string) (*VideoMetadata, error)
	SaveMetadata(videoID string, metadata *VideoMetadata) error
}

func NewFileVideoStore(dir string) VideoStore {
	return store.NewFile(dir)
}
