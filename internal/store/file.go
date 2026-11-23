// Package store persists transcripts and metadata on the local filesystem.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/rtzll/tldw/internal/atomicfile"
	"github.com/rtzll/tldw/internal/tldw"
)

const metadataCacheVersion = 4

// Earlier caption caches can contain translations, duplicated rolling windows,
// or missing speech. Reacquire them once with known track origin and cleanup.
const captionCacheVersion = 3

type cachedTranscript struct {
	CacheVersion int                      `json:"cache_version,omitempty"`
	VideoID      string                   `json:"video_id,omitempty"`
	Language     string                   `json:"language,omitempty"`
	Source       tldw.TranscriptSource    `json:"source,omitempty"`
	Text         string                   `json:"text,omitempty"`
	Segments     []tldw.TranscriptSegment `json:"segments,omitempty"`
	CaptionTrack *tldw.CaptionTrack       `json:"caption_track,omitempty"`
}

// File is the filesystem adapter for the application's persistence seam.
type File struct {
	dir string
}

func NewFile(dir string) *File {
	return &File{dir: dir}
}

// WithTranscriptLock coordinates acquisition across independent processes sharing
// the same store. Lock files are retained so waiters always lock the same inode.
func (s *File) WithTranscriptLock(ctx context.Context, videoID string, read func() (*tldw.Transcript, error)) (result *tldw.Transcript, err error) {
	path, err := s.cachePath(videoID, ".lock")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	lock := flock.New(path)
	locked, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, ctx.Err()
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	return read()
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
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: empty legacy transcript", tldw.ErrStoreStale)
	}
	return tldw.NewLegacyTranscript(videoID, text, nil)
}

func (s *File) SaveTranscript(transcript *tldw.Transcript) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("creating transcript store: %w", err)
	}
	plain, err := transcript.Render(tldw.TranscriptRenderFormatPlain)
	if err != nil {
		return err
	}
	if err := s.saveStructuredTranscript(transcript); err != nil {
		return err
	}
	// JSON is authoritative. Failure to update the compatibility copy does
	// not invalidate the committed transcript.
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
	if cached.HasCaptions && len(cached.CaptionTracks) == 0 {
		return nil, fmt.Errorf("%w: caption origin is unknown", tldw.ErrStoreStale)
	}
	metadata := metadataFromCached(cached)
	return &metadata, nil
}

// ListMetadata returns every cached video's metadata and original cache time.
func (s *File) ListMetadata() (tldw.MetadataScan, error) {
	files, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return tldw.MetadataScan{}, nil
	}
	if err != nil {
		return tldw.MetadataScan{}, fmt.Errorf("listing metadata cache: %w", err)
	}
	var scan tldw.MetadataScan
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".meta.json") {
			continue
		}
		path := filepath.Join(s.dir, file.Name())
		videoID := strings.TrimSuffix(filepath.Base(path), ".meta.json")
		if !tldw.IsValidVideoCacheKey(videoID) {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			scan.Warnings = append(scan.Warnings, fmt.Sprintf("reading metadata cache %s: %v", file.Name(), err))
			continue
		}
		var cached cachedMetadata
		if err := json.Unmarshal(data, &cached); err != nil {
			scan.Warnings = append(scan.Warnings, fmt.Sprintf("parsing metadata cache %s: %v", file.Name(), err))
			continue
		}
		firstSeenAt, err := cachedMetadataFirstSeenAt(path, cached)
		if err != nil {
			scan.Warnings = append(scan.Warnings, fmt.Sprintf("reading metadata timestamp %s: %v", file.Name(), err))
			continue
		}
		scan.Entries = append(scan.Entries, tldw.StoredVideoMetadata{
			VideoID: videoID, Metadata: metadataFromCached(cached), FirstSeenAt: firstSeenAt,
		})
	}
	return scan, nil
}

func (s *File) SaveMetadata(videoID string, metadata *tldw.VideoMetadata) (err error) {
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
	// First-seen is a read/modify/write across processes. Use a separate
	// inode from the transcript lock, which this caller may already hold.
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("locking metadata: %w", err)
	}
	defer func() {
		if unlockErr := lock.Unlock(); unlockErr != nil {
			err = errors.Join(err, unlockErr)
		}
	}()
	now := time.Now()
	firstSeenAt := metadataFirstSeenAt(path, now)
	cached := cachedMetadata{
		CacheVersion: metadataCacheVersion, Title: metadata.Title, Description: metadata.Description,
		Channel: metadata.Channel, ChannelURL: metadata.ChannelURL, Creators: metadata.Creators,
		PublishedAt: metadata.PublishedAt, Duration: metadata.Duration, Language: metadata.Language,
		Categories: metadata.Categories, Tags: metadata.Tags, Chapters: metadata.Chapters,
		HasCaptions: metadata.HasCaptions, CaptionLanguages: metadata.CaptionLanguages,
		CaptionTracks: metadata.CaptionTracks,
		FirstSeenAt:   firstSeenAt, UpdatedAt: now, CachedAt: now,
	}
	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}
	if err := atomicfile.Replace(path, data, 0o644); err != nil {
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
	if !tldw.IsValidVideoCacheKey(videoID) {
		return "", fmt.Errorf("invalid video cache key: %q", videoID)
	}
	return filepath.Join(s.dir, videoID+suffix), nil
}

func (s *File) savePlainTranscript(videoID, transcript string) error {
	path, err := s.cachePath(videoID, ".txt")
	if err != nil {
		return err
	}
	if err := atomicfile.Replace(path, []byte(transcript), 0o644); err != nil {
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
	path, err := s.cachePath(transcript.VideoID, ".transcript.json")
	if err != nil {
		return fmt.Errorf("saving transcript: %w", err)
	}
	cached := cachedTranscript{VideoID: transcript.VideoID, Language: transcript.Language, Source: transcript.Source(), Text: transcript.PlainText(), Segments: transcript.Segments(), CaptionTrack: transcript.CaptionTrack()}
	if transcript.Source() == tldw.TranscriptSourceCaptions {
		cached.CacheVersion = captionCacheVersion
	}
	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling transcript: %w", err)
	}
	if err := atomicfile.Replace(path, data, 0o644); err != nil {
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
	var cached cachedTranscript
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("parsing structured transcript: %w", err)
	}
	if cached.Source == tldw.TranscriptSourceCaptions && cached.CacheVersion < captionCacheVersion {
		return nil, fmt.Errorf("%w: caption version %d", tldw.ErrStoreStale, cached.CacheVersion)
	}
	if cached.Source == tldw.TranscriptSourceCaptions && (cached.CaptionTrack == nil || !cached.CaptionTrack.Direct) {
		return nil, fmt.Errorf("%w: caption origin is unknown or translated", tldw.ErrStoreStale)
	}
	if cached.VideoID != "" && cached.VideoID != videoID {
		return nil, fmt.Errorf("transcript cache key %q contains %q", videoID, cached.VideoID)
	}
	var transcript *tldw.Transcript
	switch cached.Source {
	case "whisper", tldw.TranscriptSourcePaidTranscription:
		// Paid records have no caption provenance. Extract legacy segmented
		// text before discarding timing from older wire formats.
		text := cached.Text
		if len(cached.Segments) > 0 {
			legacy, decodeErr := tldw.NewLegacyTranscript(videoID, text, cached.Segments)
			if decodeErr != nil {
				return nil, fmt.Errorf("%w: %w", tldw.ErrStoreStale, decodeErr)
			}
			text = legacy.PlainText()
		}
		transcript, err = tldw.NewPaidTranscript(videoID, text)
	case tldw.TranscriptSourceCaptions:
		transcript, err = tldw.NewCaptionTranscript(videoID, *cached.CaptionTrack, cached.Text, cached.Segments)
	default:
		transcript, err = tldw.NewLegacyTranscript(videoID, cached.Text, cached.Segments)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", tldw.ErrStoreStale, err)
	}
	if cached.Language != "" {
		transcript.Language = cached.Language
	}
	return transcript, nil
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
	CaptionTracks    []tldw.CaptionTrack `json:"caption_tracks,omitempty"`
	FirstSeenAt      time.Time           `json:"first_seen_at,omitempty"`
	UpdatedAt        time.Time           `json:"updated_at,omitempty"`
	CachedAt         time.Time           `json:"cached_at"`
}

func metadataFromCached(cached cachedMetadata) tldw.VideoMetadata {
	checkedAt := cached.UpdatedAt
	if checkedAt.IsZero() {
		checkedAt = cached.CachedAt
	}
	return tldw.VideoMetadata{
		CheckedAt: checkedAt,
		Title:     cached.Title, Description: cached.Description, Channel: cached.Channel,
		ChannelURL: cached.ChannelURL, Creators: cached.Creators, PublishedAt: cached.PublishedAt,
		Duration: cached.Duration, Language: cached.Language, Categories: cached.Categories,
		Tags: cached.Tags, Chapters: cached.Chapters, HasCaptions: cached.HasCaptions,
		CaptionLanguages: cached.CaptionLanguages,
		CaptionTracks:    cached.CaptionTracks,
	}
}
