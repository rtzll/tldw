package ytdlp

import (
	"context"
	"errors"
	"time"

	"github.com/rtzll/tldw/internal/process"
	"github.com/rtzll/tldw/internal/tldw"
)

type discardLogSink struct{}

func (discardLogSink) Printf(string, ...any) {}

// Client implements video access through yt-dlp.
type Client struct {
	transcriptsDir string
	cacheDir       string
	verbose        bool
	quiet          bool
	log            tldw.LogSink
	executor       process.Runner
	youtube        *tldw.YouTubeCooldown
}

// New creates a yt-dlp client with explicit storage paths.
func New(transcriptsDir, cacheDir string, verbose bool, quiet bool) *Client {
	return &Client{
		transcriptsDir: transcriptsDir,
		cacheDir:       cacheDir,
		verbose:        verbose,
		quiet:          quiet,
		log:            discardLogSink{},
		executor:       &process.CommandRunner{},
		youtube:        &tldw.YouTubeCooldown{},
	}
}

func (yt *Client) SetLogSink(log tldw.LogSink) {
	yt.log = log
}

func (yt *Client) FetchMetadata(ctx context.Context, ref tldw.ContentRef) (*tldw.VideoMetadata, error) {
	return readVideo(ctx, yt, ref, func(ctx context.Context) (*tldw.VideoMetadata, error) {
		return yt.metadata(ctx, ref)
	})
}

// Only metadata and uncached caption reads participate in YouTube recovery.
func readVideo[T any](ctx context.Context, yt *Client, ref tldw.ContentRef, fetch func(context.Context) (T, error)) (T, error) {
	if ref.IsX() {
		return fetch(ctx)
	}
	var value T
	var limitedAt time.Time
	err := yt.youtube.Read(ctx, func(attemptCtx context.Context) error {
		var err error
		value, err = fetch(attemptCtx)
		if !limitedAt.IsZero() {
			yt.log.Printf("YouTube rate-limit retry recovered=%t added_time=%s\n", err == nil, time.Since(limitedAt).Round(time.Millisecond))
		} else if errors.Is(err, tldw.ErrRateLimited) {
			limitedAt = time.Now()
		}
		return err
	})
	return value, err
}

func (yt *Client) FetchCaptions(ctx context.Context, ref tldw.ContentRef, metadata *tldw.VideoMetadata) (*tldw.Transcript, error) {
	return yt.fetchStructuredTranscript(ctx, ref, metadata)
}

func (yt *Client) DownloadAudio(ctx context.Context, ref tldw.ContentRef) (string, error) {
	return yt.audio(ctx, ref)
}

func (yt *Client) FetchPlaylist(ctx context.Context, ref tldw.ContentRef) (*tldw.PlaylistInfo, error) {
	return yt.playlistVideoURLs(ctx, ref)
}

func extractorArgs(ref tldw.ContentRef) []string {
	if ref.IsX() {
		// A text-only post can link to an external page. Only resolve X videos.
		return []string{"--use-extractors", "twitter"}
	}
	// Let the installed yt-dlp select compatible YouTube players for both
	// track discovery and download.
	return nil
}

func lookupArgs(ref tldw.ContentRef) []string {
	return append([]string{"--sleep-interval", "1", "--max-sleep-interval", "3", "-q"}, extractorArgs(ref)...)
}
