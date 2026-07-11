package ytdlp

import (
	"context"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/rtzll/tldw/internal/process"
	"github.com/rtzll/tldw/internal/tldw"
)

// ErrDownloadFailed indicates a retryable download failure from yt-dlp.
var ErrDownloadFailed = tldw.ErrDownloadFailed

type VideoMetadata = tldw.VideoMetadata
type VideoChapter = tldw.VideoChapter
type YouTubeRef = tldw.YouTubeRef
type Transcript = tldw.Transcript
type TranscriptSegment = tldw.TranscriptSegment
type PlaylistInfo = tldw.PlaylistInfo

const (
	ContentTypeVideo            = tldw.ContentTypeVideo
	TranscriptSourceCaptions    = tldw.TranscriptSourceCaptions
	TranscriptRenderFormatPlain = tldw.TranscriptRenderFormatPlain
)

type discardLogSink struct{}

func (discardLogSink) Printf(string, ...any) {}

// YouTube implements video access through yt-dlp.
type YouTube struct {
	transcriptsDir string
	cacheDir       string
	verbose        bool
	quiet          bool
	log            tldw.LogSink
	executor       process.Runner
}

func defaultYouTubeCacheDir() string {
	return filepath.Join(xdg.CacheHome, "tldw")
}

// NewYouTube creates a new YouTube downloader.
func NewYouTube(transcriptsDir string, verbose bool, quiet bool) *YouTube {
	return NewYouTubeWithCache(transcriptsDir, defaultYouTubeCacheDir(), verbose, quiet)
}

// NewYouTubeWithCache creates a new YouTube downloader with an explicit cache directory.
func NewYouTubeWithCache(transcriptsDir, cacheDir string, verbose bool, quiet bool) *YouTube {
	if cacheDir == "" {
		cacheDir = defaultYouTubeCacheDir()
	}
	return &YouTube{
		transcriptsDir: transcriptsDir,
		cacheDir:       cacheDir,
		verbose:        verbose,
		quiet:          quiet,
		log:            discardLogSink{},
		executor:       &process.CommandRunner{},
	}
}

func (yt *YouTube) SetLogSink(log tldw.LogSink) {
	yt.log = log
}

func (yt *YouTube) FetchMetadata(ctx context.Context, ref YouTubeRef) (*VideoMetadata, error) {
	return yt.metadata(ctx, ref)
}

func (yt *YouTube) FetchCaptions(ctx context.Context, ref YouTubeRef, preferredLangs []string, originalLang string) (*Transcript, error) {
	return yt.fetchStructuredTranscript(ctx, ref, preferredLangs, originalLang)
}

func (yt *YouTube) DownloadAudio(ctx context.Context, ref YouTubeRef) (string, error) {
	return yt.audio(ctx, ref)
}

func (yt *YouTube) FetchPlaylist(ctx context.Context, ref YouTubeRef) (*PlaylistInfo, error) {
	return yt.playlistVideoURLs(ctx, ref)
}
