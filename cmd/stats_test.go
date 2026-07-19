package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rtzll/tldw/internal/tldw"
)

type statsApplicationStub struct {
	report tldw.StatsReport
	query  tldw.StatsQuery
}

func (stub *statsApplicationStub) Stats(query tldw.StatsQuery) (tldw.StatsReport, error) {
	stub.query = query
	return stub.report, nil
}

func TestStatsCommandReportsThisMonthAndGroupsByDay(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	stub := &statsApplicationStub{report: tldw.StatsReport{
		VideoCount: 2, DurationSeconds: 5400,
		Groups: []tldw.StatsBucket{{Label: "2026-07-02", VideoCount: 2, DurationSeconds: 5400}},
	}}
	command := newStatsCommand(
		func() (statsApplication, error) { return stub, nil },
		func() time.Time { return time.Date(2026, time.July, 19, 11, 0, 0, 0, berlin) },
	)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--period", "month", "--group-by", "day"})

	if err := command.Execute(); err != nil {
		t.Fatalf("stats command error = %v", err)
	}
	want := "TLDW stats — this month\n2 unique videos\nVideo runtime processed: 1h 30m\nEstimated watch time avoided: about 1h 30m\n\nBy day:\n2026-07-02  2 videos  1h 30m\n"
	if output.String() != want {
		t.Fatalf("stats output = %q, want %q", output.String(), want)
	}
	wantFrom := time.Date(2026, time.July, 1, 0, 0, 0, 0, berlin)
	wantTo := time.Date(2026, time.August, 1, 0, 0, 0, 0, berlin)
	if !stub.query.From.Equal(wantFrom) || !stub.query.To.Equal(wantTo) || stub.query.GroupBy != tldw.StatsGroupDay || stub.query.Location != berlin {
		t.Fatalf("Stats() query = %+v", stub.query)
	}
}

func TestStatsCommandReturnsStructuredJSON(t *testing.T) {
	stub := &statsApplicationStub{report: tldw.StatsReport{VideoCount: 1, DurationSeconds: 90}}
	command := newStatsCommand(
		func() (statsApplication, error) { return stub, nil },
		func() time.Time { return time.Date(2026, time.July, 19, 11, 0, 0, 0, time.UTC) },
	)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--period", "all", "--json"})

	if err := command.Execute(); err != nil {
		t.Fatalf("stats command error = %v", err)
	}
	for _, fragment := range []string{`"period": "all"`, `"video_count": 1`, `"duration_seconds": 90`, `"estimated_time_saved_seconds": 90`} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("JSON output %q does not contain %q", output.String(), fragment)
		}
	}
}

func TestResolveStatsPeriodUsesLocalCalendarBoundaries(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	now := time.Date(2026, time.July, 19, 23, 30, 0, 0, berlin) // Sunday
	tests := []struct {
		name     string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{
			name:     "today",
			wantFrom: time.Date(2026, time.July, 19, 0, 0, 0, 0, berlin),
			wantTo:   time.Date(2026, time.July, 20, 0, 0, 0, 0, berlin),
		},
		{
			name:     "week",
			wantFrom: time.Date(2026, time.July, 13, 0, 0, 0, 0, berlin),
			wantTo:   time.Date(2026, time.July, 20, 0, 0, 0, 0, berlin),
		},
		{
			name:     "month",
			wantFrom: time.Date(2026, time.July, 1, 0, 0, 0, 0, berlin),
			wantTo:   time.Date(2026, time.August, 1, 0, 0, 0, 0, berlin),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			period, err := resolveStatsPeriod(tt.name, now)
			if err != nil {
				t.Fatalf("resolveStatsPeriod() error = %v", err)
			}
			if !period.from.Equal(tt.wantFrom) || !period.to.Equal(tt.wantTo) {
				t.Fatalf("resolveStatsPeriod() = %v to %v, want %v to %v", period.from, period.to, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

func TestStatsCommandRejectsUnknownPeriodAndGrouping(t *testing.T) {
	build := func() (statsApplication, error) { return &statsApplicationStub{}, nil }
	now := func() time.Time { return time.Now() }
	for _, args := range [][]string{{"--period", "year"}, {"--group-by", "channel"}} {
		command := newStatsCommand(build, now)
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("stats command accepted arguments %v", args)
		}
	}
}
