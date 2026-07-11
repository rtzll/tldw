package ytdlp

import (
	"context"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestPlaylistVideoURLsSkipsInvalidVideoIDs(t *testing.T) {
	yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
	yt.executor = &mockCommandRunner{output: []byte(`{
		"title":"Playlist",
		"entries":[
			{"id":"dQw4w9WgXcQ","title":"valid"},
			{"id":"../../outside","title":"invalid"}
		]
	}`)}
	ref, err := tldw.ParseReference("PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq")
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}

	info, err := yt.FetchPlaylist(context.Background(), ref)
	if err != nil {
		t.Fatalf("FetchPlaylist() error = %v", err)
	}
	if len(info.Videos) != 1 {
		t.Fatalf("Videos length = %d, want 1 (%v)", len(info.Videos), info.Videos)
	}
	if info.Videos[0].URL() != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("Videos[0] = %+v", info.Videos[0])
	}
}
