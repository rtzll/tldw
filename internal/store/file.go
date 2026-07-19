// Package store persists transcripts and metadata on the local filesystem.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rtzll/tldw/internal/tldw"
)

const metadataCacheVersion = 3

// File is the filesystem adapter for the application's persistence seam.
type File struct {
	dir string
}

func NewFile(dir string) *File {
	return &File{dir: dir}
}

func (s *File) LoadTranscript(videoID string) (*tldw.Transcript, error) {
	transcript, err := s.loadStructuredTranscript(videoID)
	if err == nil || !errors.Is(err, tldw.ErrStoreNotFound) {
		return transcript, err
	}
	text, err := s.loadPlainTranscript(videoID)
	if err != nil {
		return nil, err
	}
	return &tldw.Transcript{VideoID: videoID, Text: text}, nil
}

func (s *File) SaveTranscript(transcript *tldw.Transcript) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("creating transcript store: %w", err)
	}
	if err := s.saveStructuredTranscript(transcript); err != nil {
		return err
	}
	plain, err := transcript.Render(tldw.TranscriptRenderFormatPlain)
	if err != nil {
		return err
	}
	return s.savePlainTranscript(transcript.VideoID, plain)
}

func (s *File) LoadMetadata(videoID string) (*tldw.VideoMetadata, error) {
	path, err := s.cachePath(videoID, ".meta.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: metadata %s", tldw.ErrStoreNotFound, videoID)
	}
	if err != nil {
		return nil, fmt.Errorf("reading metadata cache: %w", err)
	}
	var cached cachedMetadata
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("parsing metadata cache: %w", err)
	}
	if cached.CacheVersion < metadataCacheVersion {
		return nil, fmt.Errorf("%w: metadata version %d", tldw.ErrStoreStale, cached.CacheVersion)
	}
	metadata := metadataFromCached(cached)
	return &metadata, nil
}

// ListMetadata returns every cached video's metadata and original cache time.
func (s *File) ListMetadata() ([]tldw.StoredVideoMetadata, error) {
	files, err := filepath.Glob(filepath.Join(s.dir, "*.meta.json"))
	if err != nil {
		return nil, fmt.Errorf("listing metadata cache: %w", err)
	}
	entries := make([]tldw.StoredVideoMetadata, 0, len(files))
	for _, path := range files {
		videoID := strings.TrimSuffix(filepath.Base(path), ".meta.json")
		if !tldw.IsValidVideoID(videoID) {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("reading metadata cache %s: %w", filepath.Base(path), err)
		}
		var cached cachedMetadata
		if err := json.Unmarshal(data, &cached); err != nil {
			return nil, fmt.Errorf("parsing metadata cache %s: %w", filepath.Base(path), err)
		}
		firstSeenAt, err := cachedMetadataFirstSeenAt(path, cached)
		if err != nil {
			return nil, fmt.Errorf("reading metadata timestamp %s: %w", filepath.Base(path), err)
		}
		entries = append(entries, tldw.StoredVideoMetadata{
			VideoID: videoID, Metadata: metadataFromCached(cached), FirstSeenAt: firstSeenAt,
		})
	}
	return entries, nil
}

func (s *File) SaveMetadata(videoID string, metadata *tldw.VideoMetadata) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("creating metadata store: %w", err)
	}
	path, err := s.cachePath(videoID, ".meta.json")
	if err != nil {
		return err
	}
	if metadata == nil {
		return fmt.Errorf("saving metadata: metadata is nil")
	}
	now := time.Now()
	firstSeenAt := metadataFirstSeenAt(path, now)
	cached := cachedMetadata{
		CacheVersion: metadataCacheVersion, Title: metadata.Title, Description: metadata.Description,
		Channel: metadata.Channel, ChannelURL: metadata.ChannelURL, Creators: metadata.Creators,
		PublishedAt: metadata.PublishedAt, Duration: metadata.Duration, Language: metadata.Language,
		Categories: metadata.Categories, Tags: metadata.Tags, Chapters: metadata.Chapters,
		HasCaptions: metadata.HasCaptions, CaptionLanguages: metadata.CaptionLanguages,
		FirstSeenAt: firstSeenAt, UpdatedAt: now, CachedAt: now,
	}
	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("saving metadata: %w", err)
	}
	return nil
}

func metadataFirstSeenAt(path string, fallback time.Time) time.Time {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fallback
	}
	var cached cachedMetadata
	if err := json.Unmarshal(data, &cached); err != nil {
		return fallback
	}
	firstSeenAt, err := cachedMetadataFirstSeenAt(path, cached)
	if err != nil {
		return fallback
	}
	return firstSeenAt
}

func cachedMetadataFirstSeenAt(path string, cached cachedMetadata) (time.Time, error) {
	if !cached.FirstSeenAt.IsZero() {
		return cached.FirstSeenAt, nil
	}
	if !cached.CachedAt.IsZero() {
		return cached.CachedAt, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

func (s *File) cachePath(videoID, suffix string) (string, error) {
	if !tldw.IsValidVideoID(videoID) {
		return "", fmt.Errorf("invalid YouTube video ID: %q", videoID)
	}
	return filepath.Join(s.dir, videoID+suffix), nil
}

func (s *File) savePlainTranscript(videoID, transcript string) error {
	path, err := s.cachePath(videoID, ".txt")
	if err != nil {
		return err
	}
	if err := atomicWriteFile(path, []byte(transcript), 0o644); err != nil {
		return fmt.Errorf("saving transcript: %w", err)
	}
	return nil
}

func (s *File) loadPlainTranscript(videoID string) (string, error) {
	path, err := s.cachePath(videoID, ".txt")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%w: transcript %s", tldw.ErrStoreNotFound, videoID)
	}
	if err != nil {
		return "", fmt.Errorf("reading legacy transcript: %w", err)
	}
	return string(data), nil
}

func (s *File) saveStructuredTranscript(transcript *tldw.Transcript) error {
	if transcript == nil {
		return fmt.Errorf("saving transcript: transcript is nil")
	}
	path, err := s.cachePath(transcript.VideoID, ".transcript.json")
	if err != nil {
		return fmt.Errorf("saving transcript: %w", err)
	}
	data, err := json.MarshalIndent(transcript, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling transcript: %w", err)
	}
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("saving structured transcript: %w", err)
	}
	return nil
}

func (s *File) loadStructuredTranscript(videoID string) (*tldw.Transcript, error) {
	path, err := s.cachePath(videoID, ".transcript.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: transcript %s", tldw.ErrStoreNotFound, videoID)
	}
	if err != nil {
		return nil, fmt.Errorf("reading structured transcript: %w", err)
	}
	var transcript tldw.Transcript
	if err := json.Unmarshal(data, &transcript); err != nil {
		return nil, fmt.Errorf("parsing structured transcript: %w", err)
	}
	if transcript.VideoID == "" {
		transcript.VideoID = videoID
	}
	return &transcript, nil
}

type cachedMetadata struct {
	CacheVersion     int                 `json:"cache_version"`
	Title            string              `json:"title"`
	Description      string              `json:"description"`
	Channel          string              `json:"channel"`
	ChannelURL       string              `json:"channel_url,omitempty"`
	Creators         []string            `json:"creators,omitempty"`
	PublishedAt      string              `json:"published_at,omitempty"`
	Duration         float64             `json:"duration"`
	Language         string              `json:"language"`
	Categories       []string            `json:"categories"`
	Tags             []string            `json:"tags"`
	Chapters         []tldw.VideoChapter `json:"chapters"`
	HasCaptions      bool                `json:"has_captions"`
	CaptionLanguages []string            `json:"caption_languages"`
	FirstSeenAt      time.Time           `json:"first_seen_at,omitempty"`
	UpdatedAt        time.Time           `json:"updated_at,omitempty"`
	CachedAt         time.Time           `json:"cached_at"`
}

func metadataFromCached(cached cachedMetadata) tldw.VideoMetadata {
	return tldw.VideoMetadata{
		Title: cached.Title, Description: cached.Description, Channel: cached.Channel,
		ChannelURL: cached.ChannelURL, Creators: cached.Creators, PublishedAt: cached.PublishedAt,
		Duration: cached.Duration, Language: cached.Language, Categories: cached.Categories,
		Tags: cached.Tags, Chapters: cached.Chapters, HasCaptions: cached.HasCaptions,
		CaptionLanguages: cached.CaptionLanguages,
	}
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".tldw-*")
	if err != nil {
		return fmt.Errorf("creating temporary cache file: %w", err)
	}
	tempPath := temp.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temp.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("closing temporary cache file: %w", closeErr)
			}
		}
		if removeErr := os.Remove(tempPath); err == nil && removeErr != nil && !os.IsNotExist(removeErr) {
			err = fmt.Errorf("removing temporary cache file: %w", removeErr)
		}
	}()

	if err := temp.Chmod(mode); err != nil {
		return fmt.Errorf("setting temporary cache permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("writing temporary cache file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("syncing temporary cache file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing temporary cache file: %w", err)
	}
	closed = true
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replacing cache file: %w", err)
	}
	return nil
}
