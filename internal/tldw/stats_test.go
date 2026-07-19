package tldw_test

import (
	"testing"
	"time"

	"github.com/rtzll/tldw/internal/tldw"
)

func TestEngineStatsFiltersAndGroupsUniqueCachedVideos(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	store := &memoryStore{metadataEntries: []tldw.StoredVideoMetadata{
		{VideoID: "aaaaaaaaaaa", Metadata: tldw.VideoMetadata{Duration: 3600}, FirstSeenAt: time.Date(2026, time.July, 1, 22, 30, 0, 0, time.UTC)},
		{VideoID: "bbbbbbbbbbb", Metadata: tldw.VideoMetadata{Duration: 1800}, FirstSeenAt: time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)},
		{VideoID: "ccccccccccc", Metadata: tldw.VideoMetadata{Duration: 7200}, FirstSeenAt: time.Date(2026, time.June, 30, 8, 0, 0, 0, time.UTC)},
	}}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: &videoStub{}, Store: store, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	report, err := engine.Stats(tldw.StatsQuery{
		From:     time.Date(2026, time.July, 1, 0, 0, 0, 0, berlin),
		To:       time.Date(2026, time.August, 1, 0, 0, 0, 0, berlin),
		GroupBy:  tldw.StatsGroupDay,
		Location: berlin,
	})
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if report.VideoCount != 2 || report.DurationSeconds != 5400 {
		t.Fatalf("Stats() = %+v, want 2 videos and 5400 seconds", report)
	}
	if len(report.Groups) != 1 || report.Groups[0].Label != "2026-07-02" || report.Groups[0].VideoCount != 2 || report.Groups[0].DurationSeconds != 5400 {
		t.Fatalf("Stats() groups = %+v, want one July 2 group", report.Groups)
	}
}

func TestEngineStatsSupportsWeekAndMonthGrouping(t *testing.T) {
	store := &memoryStore{metadataEntries: []tldw.StoredVideoMetadata{
		{VideoID: "aaaaaaaaaaa", Metadata: tldw.VideoMetadata{Duration: 60}, FirstSeenAt: time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)},
		{VideoID: "bbbbbbbbbbb", Metadata: tldw.VideoMetadata{Duration: 120}, FirstSeenAt: time.Date(2026, time.July, 6, 12, 0, 0, 0, time.UTC)},
		{VideoID: "ccccccccccc", Metadata: tldw.VideoMetadata{Duration: 180}, FirstSeenAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)},
	}}
	engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{
		Video: &videoStub{}, Store: store, AI: &aiStub{}, Prompts: &promptStub{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	tests := []struct {
		name   string
		group  tldw.StatsGroup
		labels []string
	}{
		{name: "week", group: tldw.StatsGroupWeek, labels: []string{"2026-06-29", "2026-07-06", "2026-07-27"}},
		{name: "month", group: tldw.StatsGroupMonth, labels: []string{"2026-07", "2026-08"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := engine.Stats(tldw.StatsQuery{GroupBy: tt.group, Location: time.UTC})
			if err != nil {
				t.Fatalf("Stats() error = %v", err)
			}
			if len(report.Groups) != len(tt.labels) {
				t.Fatalf("Stats() groups = %+v, want labels %v", report.Groups, tt.labels)
			}
			for i, label := range tt.labels {
				if report.Groups[i].Label != label {
					t.Fatalf("Stats() groups = %+v, want labels %v", report.Groups, tt.labels)
				}
			}
		})
	}
}
