package tldw

import (
	"fmt"
	"sort"
	"time"
)

// StatsGroup controls how matching videos are bucketed in a stats report.
type StatsGroup string

const (
	StatsGroupNone  StatsGroup = ""
	StatsGroupDay   StatsGroup = "day"
	StatsGroupWeek  StatsGroup = "week"
	StatsGroupMonth StatsGroup = "month"
)

// StatsQuery selects cached videos by their first-seen time. From is inclusive
// and To is exclusive. Zero values leave the corresponding bound open.
type StatsQuery struct {
	From     time.Time
	To       time.Time
	GroupBy  StatsGroup
	Location *time.Location
}

// StatsBucket is one calendar bucket in a grouped stats report.
type StatsBucket struct {
	Label           string  `json:"label"`
	VideoCount      int     `json:"video_count"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// StatsReport summarizes unique videos in the local metadata library.
type StatsReport struct {
	VideoCount      int           `json:"video_count"`
	DurationSeconds float64       `json:"duration_seconds"`
	Groups          []StatsBucket `json:"groups,omitempty"`
}

// Stats calculates unique-video statistics from locally cached metadata.
func (app *Engine) Stats(query StatsQuery) (StatsReport, error) {
	if err := validateStatsQuery(query); err != nil {
		return StatsReport{}, err
	}
	entries, err := app.store.ListMetadata()
	if err != nil {
		return StatsReport{}, fmt.Errorf("listing cached metadata: %w", err)
	}

	report := StatsReport{}
	grouped := make(map[string]StatsBucket)
	for _, entry := range entries {
		if (!query.From.IsZero() && entry.FirstSeenAt.Before(query.From)) ||
			(!query.To.IsZero() && !entry.FirstSeenAt.Before(query.To)) {
			continue
		}
		report.VideoCount++
		report.DurationSeconds += entry.Metadata.Duration
		if query.GroupBy == StatsGroupNone {
			continue
		}
		label := statsGroupLabel(entry.FirstSeenAt, query.GroupBy, query.Location)
		bucket := grouped[label]
		bucket.Label = label
		bucket.VideoCount++
		bucket.DurationSeconds += entry.Metadata.Duration
		grouped[label] = bucket
	}

	if query.GroupBy != StatsGroupNone {
		report.Groups = make([]StatsBucket, 0, len(grouped))
		for _, bucket := range grouped {
			report.Groups = append(report.Groups, bucket)
		}
		sort.Slice(report.Groups, func(i, j int) bool {
			return report.Groups[i].Label < report.Groups[j].Label
		})
	}
	return report, nil
}

func validateStatsQuery(query StatsQuery) error {
	switch query.GroupBy {
	case StatsGroupNone, StatsGroupDay, StatsGroupWeek, StatsGroupMonth:
	default:
		return fmt.Errorf("unsupported stats grouping: %q", query.GroupBy)
	}
	if !query.From.IsZero() && !query.To.IsZero() && !query.From.Before(query.To) {
		return fmt.Errorf("stats start time must be before end time")
	}
	return nil
}

func statsGroupLabel(timestamp time.Time, group StatsGroup, location *time.Location) string {
	if location == nil {
		location = time.Local
	}
	local := timestamp.In(location)
	switch group {
	case StatsGroupDay:
		return local.Format("2006-01-02")
	case StatsGroupWeek:
		weekdayOffset := (int(local.Weekday()) + 6) % 7
		weekStart := local.AddDate(0, 0, -weekdayOffset)
		return weekStart.Format("2006-01-02")
	case StatsGroupMonth:
		return local.Format("2006-01")
	default:
		return ""
	}
}
