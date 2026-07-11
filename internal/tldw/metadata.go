package tldw

// VideoMetadata contains transport-neutral YouTube video information.
type VideoMetadata struct {
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

	// CacheVersion tracks on-disk metadata schema compatibility.
	CacheVersion int `json:"-"`
}

// VideoChapter represents a video chapter marker.
type VideoChapter struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Title     string  `json:"title"`
}
