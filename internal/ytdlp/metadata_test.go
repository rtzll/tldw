package ytdlp

import (
	"context"
	"strings"
	"testing"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestMetadataUsesChannelURL(t *testing.T) {
	tests := []struct {
		name           string
		json           string
		wantChannelURL string
	}{
		{
			name:           "channel url",
			json:           `{"title":"Test","channel_url":"https://www.youtube.com/channel/UCLKPca3kwwd-B59HNr-_lvA","uploader_url":"https://www.youtube.com/@aiDotEngineer"}`,
			wantChannelURL: "https://www.youtube.com/channel/UCLKPca3kwwd-B59HNr-_lvA",
		},
		{
			name:           "uploader url fallback",
			json:           `{"title":"Test","channel_url":"","uploader_url":"https://www.youtube.com/@aiDotEngineer"}`,
			wantChannelURL: "https://www.youtube.com/@aiDotEngineer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
			yt.executor = &mockCommandRunner{output: []byte(tt.json)}
			ref, err := tldw.ParseVideoRef("dQw4w9WgXcQ")
			if err != nil {
				t.Fatalf("ParseVideoRef() error = %v", err)
			}

			metadata, err := yt.FetchMetadata(context.Background(), ref)
			if err != nil {
				t.Fatalf("FetchMetadata() error = %v", err)
			}
			if metadata.ChannelURL != tt.wantChannelURL {
				t.Fatalf("ChannelURL = %q, want %q", metadata.ChannelURL, tt.wantChannelURL)
			}
		})
	}
}

func TestMetadataUsesUploadDateAsPublishedAt(t *testing.T) {
	yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
	yt.executor = &mockCommandRunner{output: []byte(`{"title":"Test","upload_date":"20260629"}`)}
	ref, err := tldw.ParseVideoRef("dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ParseVideoRef() error = %v", err)
	}

	metadata, err := yt.FetchMetadata(context.Background(), ref)
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v", err)
	}
	if metadata.PublishedAt != "2026-06-29" {
		t.Fatalf("PublishedAt = %q, want 2026-06-29", metadata.PublishedAt)
	}
}

func TestMetadataFallsBackToUploaderWhenChannelMissing(t *testing.T) {
	tests := []struct {
		name         string
		json         string
		wantChannel  string
		wantCreators []string
	}{
		{
			name:         "channel",
			json:         `{"title":"Test","channel":"Upload Channel","uploader":"AI Engineer","creators":["AI Engineer","Matt Pocock"]}`,
			wantChannel:  "Upload Channel",
			wantCreators: []string{"AI Engineer", "Matt Pocock"},
		},
		{
			name:         "uploader",
			json:         `{"title":"Test","channel":"","uploader":"AI Engineer","creator":"AI Engineer, Matt Pocock","creators":["AI Engineer","Matt Pocock"]}`,
			wantChannel:  "AI Engineer",
			wantCreators: []string{"AI Engineer", "Matt Pocock"},
		},
		{
			name:         "creators do not populate channel",
			json:         `{"title":"Test","channel":" ","uploader":"","creator":"","creators":["AI Engineer","Matt Pocock"]}`,
			wantChannel:  "",
			wantCreators: []string{"AI Engineer", "Matt Pocock"},
		},
		{
			name:         "creator does not populate channel",
			json:         `{"title":"Test","channel":"","uploader":"","creator":"AI Engineer, Matt Pocock"}`,
			wantChannel:  "",
			wantCreators: []string{"AI Engineer, Matt Pocock"},
		},
		{
			name:         "creators string does not populate channel",
			json:         `{"title":"Test","channel":"","uploader":"","creator":"","creators":"AI Engineer, Matt Pocock"}`,
			wantChannel:  "",
			wantCreators: []string{"AI Engineer, Matt Pocock"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yt := NewYouTube(t.TempDir(), t.TempDir(), false, true)
			yt.executor = &mockCommandRunner{output: []byte(tt.json)}
			ref, err := tldw.ParseVideoRef("dQw4w9WgXcQ")
			if err != nil {
				t.Fatalf("ParseVideoRef() error = %v", err)
			}

			metadata, err := yt.FetchMetadata(context.Background(), ref)
			if err != nil {
				t.Fatalf("FetchMetadata() error = %v", err)
			}
			if metadata.Channel != tt.wantChannel {
				t.Fatalf("Channel = %q, want %q", metadata.Channel, tt.wantChannel)
			}
			if strings.Join(metadata.Creators, "|") != strings.Join(tt.wantCreators, "|") {
				t.Fatalf("Creators = %#v, want %#v", metadata.Creators, tt.wantCreators)
			}
		})
	}
}
