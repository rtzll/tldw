package ytdlp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/rtzll/tldw/internal/tldw"
)

// downloadCaptions requests exactly one language and one caption source.
func (yt *Client) downloadCaptions(ctx context.Context, ref tldw.ContentRef, track directTrack, prepared []byte) (string, error) {
	if err := os.MkdirAll(yt.cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}
	path := filepath.Join(yt.cacheDir, ref.CacheKey()+"."+track.Language+".srt")
	args := []string{"--write-subs", "--no-write-auto-subs"}
	if track.Automatic {
		args = []string{"--no-write-subs", "--write-auto-subs"}
	}
	args = append(args, extractorArgs(ref)...)
	if !ref.IsX() {
		// Space out YouTube subtitle requests to reduce rate limiting.
		args = append(args, "--sleep-subtitles", "5")
	}
	args = append(args,
		"--sub-langs", track.Language,
		"--convert-subs", "srt",
		"--skip-download",
		"-o", filepath.Join(yt.cacheDir, ref.CacheKey()),
	)
	var input io.Reader
	if prepared != nil {
		// In info-file mode the playlist default can swallow the subtitle HTTP
		// diagnostic. Abort on this one video so a 429 reaches the cooldown.
		args = append(args, "--abort-on-error", "--no-clean-info-json", "--load-info-json", "-")
		input = bytes.NewReader(prepared)
	} else {
		args = append(args, ref.URL())
	}
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Downloading direct captions: %s (automatic=%t)\n", track.Language, track.Automatic)
	}
	output, runErr := yt.run(ctx, "captions", ref, args, input, attribute.String("tldw.caption.language", track.Language), attribute.Bool("tldw.caption.automatic", track.Automatic), attribute.Bool("tldw.caption.reused_metadata", prepared != nil))
	runErr = classifyReadError(ctx, ref, output, runErr)
	if terminalReadError(runErr) {
		return "", runErr
	}

	info, fileErr := os.Stat(path)
	if fileErr == nil && info.Mode().IsRegular() {
		// yt-dlp can finish writing the requested track before a nonterminal error.
		return path, nil
	}
	if runErr != nil {
		if emptyCaptionDiagnostic(failureDiagnostic(runErr, output)) {
			return "", &tldw.EmptyCaptionResponseError{Cause: runErr}
		}
		return "", fmt.Errorf("%w: %w", tldw.ErrDownloadFailed, runErr)
	}
	if os.IsNotExist(fileErr) {
		if prepared != nil {
			// This subprocess never revisited the video to confirm a missing track.
			return "", fmt.Errorf("%w: prepared caption download produced no file", tldw.ErrDownloadFailed)
		}
		return "", &tldw.CaptionTrackUnavailableError{Track: track.provenance()}
	}
	if fileErr != nil {
		return "", fmt.Errorf("checking downloaded captions: %w", fileErr)
	}
	return "", fmt.Errorf("downloaded captions are not a regular file")
}

func (yt *Client) FetchCaptions(ctx context.Context, ref tldw.ContentRef, metadata *tldw.VideoMetadata, attempt int) (*tldw.Transcript, error) {
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
		path, err := yt.findExistingTranscript(ref.CacheKey(), track.Language)
		if err != nil {
			return nil, fmt.Errorf("searching existing captions: %w", err)
		}
		if path != "" {
			trace.SpanFromContext(ctx).AddEvent("captions.local_srt_hit")
			return yt.processSrtTranscript(ctx, path, track.provenance())
		}
	}
	canReuse := attempt == 1 && !ref.IsX()
	return readVideo(ctx, yt, ref, func(ctx context.Context) (*tldw.Transcript, error) {
		var prepared []byte
		if canReuse {
			prepared = yt.prepared.take(ref.ID(), track)
			canReuse = false // A cooldown probe must rediscover the track, too.
		}
		return yt.fetchCaptionTrack(ctx, ref, track, prepared)
	})
}

func (yt *Client) fetchCaptionTrack(ctx context.Context, ref tldw.ContentRef, track directTrack, prepared []byte) (*tldw.Transcript, error) {
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
	path, err := worker.downloadCaptions(ctx, ref, track, prepared)
	if err != nil {
		return nil, err
	}
	return worker.processSrtTranscript(ctx, path, track.provenance())
}

func (yt *Client) findExistingTranscript(videoID, language string) (string, error) {
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
