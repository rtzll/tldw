package ytdlp

import (
	"encoding/json"
	"sync"
	"time"
)

// A metadata tool call can precede the transcript tool call in the same
// process. Keep a bounded, single-use handoff outside the persistent cache.
const (
	captionInfoLifetime = time.Minute
	maxPreparedVideos   = 8
	maxCaptionInfoBytes = 1 << 20
)

type preparedCaption struct {
	track   directTrack
	info    []byte
	expires time.Time
	timer   *time.Timer
}

type preparedCaptions struct {
	mu      sync.Mutex
	byVideo map[string]*preparedCaption
}

func (p *preparedCaptions) replace(id string, track directTrack, info []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if old := p.byVideo[id]; old != nil {
		old.timer.Stop()
		delete(p.byVideo, id)
	}
	if len(info) == 0 || len(info) > maxCaptionInfoBytes || len(p.byVideo) >= maxPreparedVideos {
		return
	}
	if p.byVideo == nil {
		p.byVideo = make(map[string]*preparedCaption)
	}
	entry := &preparedCaption{track: track, info: info, expires: time.Now().Add(captionInfoLifetime)}
	p.byVideo[id] = entry
	entry.timer = time.AfterFunc(captionInfoLifetime, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.byVideo[id] == entry {
			delete(p.byVideo, id)
		}
	})
}

func (p *preparedCaptions) take(id string, track directTrack) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.byVideo[id]
	if entry == nil || entry.track != track {
		return nil
	}
	entry.timer.Stop()
	delete(p.byVideo, id)
	if !time.Now().Before(entry.expires) {
		return nil
	}
	return entry.info
}

// Keep the extractor's format and header information for yt-dlp. Supply only
// the selected track. With no webpage_url, download_with_info_file propagates
// download failures to tldw instead of retrying the watch page internally.
func captionInfoJSON(output []byte, track directTrack, formats any) []byte {
	var info map[string]json.RawMessage
	if err := json.Unmarshal(output, &info); err != nil {
		return nil
	}
	for _, key := range []string{
		"webpage_url", "original_url", "additional_urls", "subtitles", "automatic_captions",
		"requested_downloads", "requested_formats", "requested_subtitles", "_filename", "filename", "filepath",
	} {
		delete(info, key)
	}
	key := "subtitles"
	if track.Automatic {
		key = "automatic_captions"
	}
	selected, err := json.Marshal(map[string]any{track.Language: formats})
	if err != nil {
		return nil
	}
	info[key] = selected
	data, err := json.Marshal(info)
	if err != nil {
		return nil
	}
	return data
}
