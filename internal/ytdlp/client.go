package ytdlp

import (
	"context"
	"errors"
	"time"

	"github.com/rtzll/tldw/internal/process"
	"github.com/rtzll/tldw/internal/tldw"
)

const youtubeExtractorPolicy = "youtube:player_client=web,android,-tv"

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
	return readVideo(ctx, yt, ref, func() (*tldw.VideoMetadata, error) {
		return yt.metadata(ctx, ref)
	})
}

// Only metadata and uncached caption reads participate in YouTube recovery.
func readVideo[T any](ctx context.Context, yt *Client, ref tldw.ContentRef, fetch func() (T, error)) (T, error) {
	if ref.IsX() {
		return fetch()
	}
	var value T
	var limitedAt time.Time
	err := yt.youtube.Read(ctx, func() error {
		var err error
		value, err = fetch()
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
	return []string{"--extractor-args", youtubeExtractorPolicy}
}

func lookupArgs(ref tldw.ContentRef) []string {
	return append([]string{"--sleep-interval", "1", "--max-sleep-interval", "3", "-q"}, extractorArgs(ref)...)
}
