package internal

import (
	"testing"
)

func TestAppShouldShowStatus(t *testing.T) {
	tests := []struct {
		name   string
		quiet  bool
		verbose bool
		want   bool
	}{
		{"normal", false, false, true},
		{"quiet", true, false, false},
		{"verbose", false, true, false},
		{"both", true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{config: &Config{Quiet: tt.quiet, Verbose: tt.verbose}}
			if got := app.shouldShowStatus(); got != tt.want {
				t.Errorf("shouldShowStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppCachedMetadata(t *testing.T) {
	app := NewApp(&Config{})
	meta := &VideoMetadata{Title: "Test"}

	app.setCachedMetadata("abc123", meta)

	got, ok := app.getCachedMetadata("abc123")
	if !ok {
		t.Fatal("expected cached metadata to be found")
	}
	if got.Title != "Test" {
		t.Errorf("getCachedMetadata() Title = %q, want %q", got.Title, "Test")
	}

	_, ok = app.getCachedMetadata("notfound")
	if ok {
		t.Error("expected no metadata for unknown key")
	}
}

func TestBuildPlaylistTranscript(t *testing.T) {
	app := NewApp(&Config{})

	videos := []VideoTranscript{
		{
			URL:         "https://youtube.com/watch?v=abc123",
			Title:       "First Video",
			Channel:     "Channel A",
			Duration:    125,
			Description: "Description A",
			Transcript:  "Transcript A",
		},
		{
			URL:         "https://youtube.com/watch?v=def456",
			Title:       "Second Video",
			Channel:     "Channel B",
			Duration:    60,
			Description: "Description B",
			Transcript:  "Transcript B",
		},
	}

	got := app.buildPlaylistTranscript("My Playlist", videos)

	// Check that key elements are present
	expectedParts := []string{
		"Playlist: My Playlist",
		"Video 1 of 2: First Video",
		"Duration: 2:05 | Channel: Channel A",
		"Description: Description A",
		"Transcript A",
		"---",
		"Video 2 of 2: Second Video",
		"Duration: 1:00 | Channel: Channel B",
		"Transcript B",
	}

	for _, part := range expectedParts {
		if !containsSubstr(got, part) {
			t.Errorf("buildPlaylistTranscript() missing expected part: %q\ngot:\n%s", part, got)
		}
	}
}

func containsSubstr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstrHelper(s, substr))
}

func containsSubstrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
