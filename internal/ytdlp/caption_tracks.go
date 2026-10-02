package ytdlp

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

var captionLanguagePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

var englishCaptionPreference = []string{"en-US", "en", "en-GB", "en-CA", "en-AU", "en-NZ"}

// selectCaptionTrack chooses one direct track. Original English automatic
// captions come first, followed by manual English and native-language captions.
func selectCaptionTrack(tracks []tldw.CaptionTrack, originalLanguage string) (tldw.CaptionTrack, bool) {
	var best tldw.CaptionTrack
	bestPriority, bestVariant := 100, 100
	found := false
	for _, track := range tracks {
		if !track.Direct || track.Language == "live_chat" || !captionLanguagePattern.MatchString(track.Language) {
			continue
		}
		language := strings.TrimSuffix(track.Language, "-orig")
		english := language == "en" || strings.HasPrefix(language, "en-")
		priority := 6
		switch {
		case english && track.Automatic && strings.HasSuffix(track.Language, "-orig"):
			priority = 0
		case english && track.Automatic:
			priority = 1
		case english:
			priority = 2
		case originalLanguage != "" && language == originalLanguage:
			priority = 3
		case track.Automatic && strings.HasSuffix(track.Language, "-orig"):
			priority = 4
		case !track.Automatic:
			priority = 5
		}
		variant := len(englishCaptionPreference)
		for i, preferred := range englishCaptionPreference {
			if language == preferred {
				variant = i
				break
			}
		}
		if track.Language == "en-orig" {
			variant = -1
		}
		if !found || priority < bestPriority || (priority == bestPriority &&
			(variant < bestVariant || (variant == bestVariant && track.Language < best.Language))) {
			best, bestPriority, bestVariant, found = track, priority, variant, true
		}
	}
	return best, found
}

func extractCaptionTracks(subtitles, automatic map[string]any) []tldw.CaptionTrack {
	var tracks []tldw.CaptionTrack
	for language, formats := range subtitles {
		if language != "live_chat" {
			tracks = append(tracks, tldw.CaptionTrack{Language: language, Direct: directCaptionFormats(formats)})
		}
	}
	for language, formats := range automatic {
		if language != "live_chat" {
			tracks = append(tracks, tldw.CaptionTrack{
				Language: language, Automatic: true,
				Direct: directCaptionFormats(formats),
			})
		}
	}
	sort.Slice(tracks, func(i, j int) bool {
		if tracks[i].Language == tracks[j].Language {
			return !tracks[i].Automatic && tracks[j].Automatic
		}
		return tracks[i].Language < tracks[j].Language
	})
	return tracks
}

// A plain language alias can be direct or translated. Require every format to
// have a valid URL without tlang; unknown formats must not enable translations.
func directCaptionFormats(raw any) bool {
	formats, ok := raw.([]any)
	if !ok || len(formats) == 0 {
		return false
	}
	for _, format := range formats {
		fields, ok := format.(map[string]any)
		if !ok {
			return false
		}
		address, _ := fields["url"].(string)
		parsed, err := url.Parse(address)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return false
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil || query.Has("tlang") {
			return false
		}
	}
	return true
}
