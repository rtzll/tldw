//go:build smoke

package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rtzll/tldw/internal/process"
	"github.com/rtzll/tldw/internal/tldw"
)

// Exercise the installed yt-dlp's info-JSON failure behavior without contacting YouTube.
func TestPreparedCaptionHTTPFailures(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var captionRequests, otherRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/captions" {
					captionRequests.Add(1)
					w.WriteHeader(status)
				} else {
					otherRequests.Add(1)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
			video := New(t.TempDir(), t.TempDir(), false, true)
			raw := fmt.Appendf(nil, `{"id":%q,"title":"fixture","extractor":"youtube","extractor_key":"Youtube","webpage_url":%q,
				"formats":[{"format_id":"mp4","ext":"mp4","url":%q}],
				"subtitles":{"en":[{"url":%q,"ext":"vtt"}]}}`, ref.ID(), server.URL+"/watch", server.URL+"/media.mp4", server.URL+"/captions")
			video.executor = &mockCommandRunner{output: raw}
			metadata, err := video.FetchMetadata(t.Context(), ref)
			if err != nil {
				t.Fatal(err)
			}
			track, ok := selectCaptionTrack(metadata.CaptionTracks, metadata.Language)
			info := video.prepared.take(ref.ID(), track)
			if !ok || info == nil {
				t.Fatal("fixture did not prepare a caption download")
			}
			video.executor = inputRunnerFunc(func(ctx context.Context, input io.Reader, name string, args ...string) ([]byte, error) {
				return (&process.CommandRunner{}).RunWithInput(ctx, input, name, append([]string{"--ignore-config", "--proxy", ""}, args...)...)
			})
			_, err = video.downloadCaptions(t.Context(), ref, track, info)
			if status == http.StatusTooManyRequests && !errors.Is(err, tldw.ErrRateLimited) {
				t.Fatalf("expected a rate limit, got %v", err)
			}
			if status == http.StatusOK {
				if _, ok := errors.AsType[*tldw.EmptyCaptionResponseError](err); !ok {
					t.Fatalf("expected an empty caption response, got %v", err)
				}
			}
			if captionRequests.Load() != 1 || otherRequests.Load() != 0 {
				t.Fatalf("yt-dlp made %d caption requests and %d unexpected requests", captionRequests.Load(), otherRequests.Load())
			}
		})
	}
}
