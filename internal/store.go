package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

// VideoStore is the local persistence seam used by the application module.
// The filesystem adapter preserves the existing on-disk formats.
type VideoStore interface {
	LoadTranscript(videoID string) (*Transcript, error)
	LoadPlainTranscript(videoID string) (string, error)
	SaveTranscript(transcript *Transcript) error
	LoadMetadata(videoID string) (*VideoMetadata, error)
	SaveMetadata(videoID string, metadata *VideoMetadata) error
}

type fileVideoStore struct {
	dir string
}

func NewFileVideoStore(dir string) VideoStore {
	return &fileVideoStore{dir: dir}
}

func (s *fileVideoStore) LoadTranscript(videoID string) (*Transcript, error) {
	return LoadStructuredTranscript(videoID, s.dir)
}

func (s *fileVideoStore) LoadPlainTranscript(videoID string) (string, error) {
	path, err := videoCachePath(videoID, s.dir, ".txt")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("reading transcript: %w", err)
	}
	return string(data), nil
}

func (s *fileVideoStore) SaveTranscript(transcript *Transcript) error {
	if err := EnsureDirs(s.dir); err != nil {
		return fmt.Errorf("creating transcript store: %w", err)
	}
	if err := SaveStructuredTranscript(transcript, s.dir); err != nil {
		return err
	}
	plain, err := transcript.Render(TranscriptRenderFormatPlain)
	if err != nil {
		return err
	}
	return SaveTranscript(transcript.VideoID, plain, s.dir)
}

func (s *fileVideoStore) LoadMetadata(videoID string) (*VideoMetadata, error) {
	return LoadCachedMetadata(videoID, s.dir)
}

func (s *fileVideoStore) SaveMetadata(videoID string, metadata *VideoMetadata) error {
	if err := EnsureDirs(s.dir); err != nil {
		return fmt.Errorf("creating metadata store: %w", err)
	}
	return SaveMetadata(videoID, metadata, s.dir)
}
