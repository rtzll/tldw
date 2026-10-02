package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

// downloadCaptions requests exactly one language and one caption source.
// Rebuilding arguments prevents fallback from modifying the conversion flag.
func (yt *YouTube) downloadCaptions(ctx context.Context, ref tldw.YouTubeRef, track tldw.CaptionTrack) (string, error) {
	if err := os.MkdirAll(yt.cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}
	path := filepath.Join(yt.cacheDir, ref.ID()+"."+track.Language+".srt")
	args := []string{"--write-subs", "--no-write-auto-subs"}
	if track.Automatic {
		args = []string{"--no-write-subs", "--write-auto-subs"}
	}
	args = append(args,
		"--sub-langs", track.Language,
		"--convert-subs", "srt",
		"--skip-download",
		"--sleep-subtitles", "5",
		"--extractor-args", youtubeExtractorPolicy,
		"-o", filepath.Join(yt.cacheDir, "%(id)s"),
		ref.URL(),
	)
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Downloading direct captions: %s (automatic=%t)\n", track.Language, track.Automatic)
	}
	_, partialFiles, err := yt.runCaptionDownload(ctx, args, path)
	if err != nil {
		return "", captionDownloadError(err)
	}
	if len(partialFiles) > 0 && yt.verbose && !yt.quiet {
		yt.log.Printf("Captions downloaded despite a nonterminal error: %s\n", track.Language)
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", &tldw.CaptionTrackUnavailableError{Track: track}
	}
	if err != nil {
		return "", fmt.Errorf("checking downloaded captions: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("downloaded captions are not a regular file")
	}
	return path, nil
}

func (yt *YouTube) runCaptionDownload(ctx context.Context, args []string, pattern string) ([]byte, []string, error) {
	output, err := yt.executor.Run(ctx, "yt-dlp", args...)
	if ctx.Err() != nil {
		return output, nil, errors.Join(err, ctx.Err())
	}
	if err == nil {
		return output, nil, nil
	}
	if terminalCaptionError(err) {
		return output, nil, err
	}
	// CommandError includes stderr; stdout alone misses yt-dlp HTTP errors.
	details := strings.ToLower(string(output) + "\n" + err.Error())
	if strings.Contains(details, "http error 429") || strings.Contains(details, "too many requests") {
		return output, nil, fmt.Errorf("%w: %w", tldw.ErrRateLimited, err)
	}

	files, globErr := filepath.Glob(pattern)
	if globErr == nil && len(files) > 0 {
		return output, files, nil
	}
	return output, nil, err
}

func (yt *YouTube) fetchStructuredTranscript(ctx context.Context, ref tldw.YouTubeRef, metadata *tldw.VideoMetadata) (*tldw.Transcript, error) {
	if metadata == nil {
		return nil, tldw.ErrCaptionsUnavailable
	}
	track, ok := selectCaptionTrack(metadata.CaptionTracks, metadata.Language)
	if !ok {
		return nil, fmt.Errorf("%w: no direct caption track", tldw.ErrCaptionsUnavailable)
	}
	// An old plain-language SRT might be manual or automatically translated.
	// Only an exact -orig filename establishes its source without a sidecar.
	if track.Automatic && strings.HasSuffix(track.Language, "-orig") {
		path, err := yt.findExistingTranscript(ref.ID(), track.Language)
		if err != nil {
			return nil, fmt.Errorf("searching existing captions: %w", err)
		}
		if path != "" {
			return yt.processSrtTranscript(path, track)
		}
	}
	if err := os.MkdirAll(yt.cacheDir, 0o755); err != nil {
		return nil, err
	}
	workDir, err := os.MkdirTemp(yt.cacheDir, "captions-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			yt.log.Printf("Warning: failed to clean download workspace: %v\n", err)
		}
	}()
	worker := *yt
	worker.cacheDir, worker.transcriptsDir = workDir, workDir
	path, err := worker.downloadCaptions(ctx, ref, track)
	if err != nil {
		return nil, err
	}
	return worker.processSrtTranscript(path, track)
}

func (yt *YouTube) findExistingTranscript(videoID, language string) (string, error) {
	for _, dir := range []string{yt.cacheDir, yt.transcriptsDir} {
		path := filepath.Join(dir, videoID+"."+language+".srt")
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", nil
}

// extractCaptionLanguages returns a sorted, de-duplicated list of caption languages.
func extractCaptionLanguages(subtitles, autoCaptions map[string]any) []string {
	langs := make(map[string]struct{})

	for lang := range subtitles {
		if lang == "live_chat" {
			continue
		}
		langs[lang] = struct{}{}
	}

	for lang := range autoCaptions {
		if lang == "live_chat" {
			continue
		}
		langs[lang] = struct{}{}
	}

	if len(langs) == 0 {
		return nil
	}

	result := make([]string, 0, len(langs))
	for lang := range langs {
		result = append(result, lang)
	}
	sort.Strings(result)
	return result
}

func terminalCaptionError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, tldw.ErrRateLimited)
}

func captionDownloadError(err error) error {
	if terminalCaptionError(err) {
		return err
	}
	return fmt.Errorf("%w: %w", tldw.ErrDownloadFailed, err)
}
