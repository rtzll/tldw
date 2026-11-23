package tldw

import (
	"slices"
	"time"
)

// VideoMetadata contains transport-neutral video information.
type VideoMetadata struct {
	// CheckedAt records when metadata was fetched; it is not part of the public JSON.
	CheckedAt        time.Time      `json:"-"`
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Channel          string         `json:"channel"`
	ChannelURL       string         `json:"channel_url,omitempty"`
	Creators         []string       `json:"creators,omitempty"`
	PublishedAt      string         `json:"published_at,omitempty"`
	Duration         float64        `json:"duration"`
	Language         string         `json:"language"`
	Categories       []string       `json:"categories"`
	Tags             []string       `json:"tags"`
	Chapters         []VideoChapter `json:"chapters"`
	HasCaptions      bool           `json:"has_captions"`
	CaptionLanguages []string       `json:"caption_languages"`
	CaptionTracks    []CaptionTrack `json:"caption_tracks,omitempty"`
}

func (m *VideoMetadata) clone() *VideoMetadata {
	copy := *m
	copy.Creators = slices.Clone(m.Creators)
	copy.Categories = slices.Clone(m.Categories)
	copy.Tags = slices.Clone(m.Tags)
	copy.Chapters = slices.Clone(m.Chapters)
	copy.CaptionLanguages = slices.Clone(m.CaptionLanguages)
	copy.CaptionTracks = slices.Clone(m.CaptionTracks)
	return &copy
}

// CaptionTrack identifies a caption language and its origin without retaining
// signed download URLs. Direct excludes automatic translations.
type CaptionTrack struct {
	Language  string `json:"language"`
	Automatic bool   `json:"automatic"`
	Direct    bool   `json:"direct"`
}

// VideoChapter represents a video chapter marker.
type VideoChapter struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Title     string  `json:"title"`
}

// StoredVideoMetadata is cached video metadata together with the time the
// video first entered the local library.
type StoredVideoMetadata struct {
	VideoID     string
	Metadata    VideoMetadata
	FirstSeenAt time.Time
}
