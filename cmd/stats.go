package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal/tldw"
)

type statsApplication interface {
	Stats(tldw.StatsQuery) (tldw.StatsReport, error)
}

type statsApplicationFactory func() (statsApplication, error)

type statsPeriod struct {
	name  string
	label string
	from  time.Time
	to    time.Time
}

type statsJSONOutput struct {
	Period                    string             `json:"period"`
	From                      string             `json:"from,omitempty"`
	To                        string             `json:"to,omitempty"`
	VideoCount                int                `json:"video_count"`
	DurationSeconds           float64            `json:"duration_seconds"`
	EstimatedTimeSavedSeconds float64            `json:"estimated_time_saved_seconds"`
	Groups                    []tldw.StatsBucket `json:"groups,omitempty"`
}

func newStatsCommand(build statsApplicationFactory, now func() time.Time) *cobra.Command {
	command := &cobra.Command{
		Use:   "stats",
		Short: "Show time saved across cached videos",
		Example: `  # Show all-time stats
  tldw stats

  # Show this month grouped by day
  tldw stats --period month --group-by day

  # Return machine-readable output
  tldw stats --period week --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			currentTime := now()
			periodName, err := cmd.Flags().GetString("period")
			if err != nil {
				return err
			}
			period, err := resolveStatsPeriod(periodName, currentTime)
			if err != nil {
				return err
			}
			groupName, err := cmd.Flags().GetString("group-by")
			if err != nil {
				return err
			}
			group, err := parseStatsGroup(groupName)
			if err != nil {
				return err
			}
			app, err := build()
			if err != nil {
				return fmt.Errorf("building application: %w", err)
			}
			report, err := app.Stats(tldw.StatsQuery{
				From: period.from, To: period.to, GroupBy: group, Location: currentTime.Location(),
			})
			if err != nil {
				return err
			}
			jsonOutput, err := cmd.Flags().GetBool("json")
			if err != nil {
				return err
			}
			if jsonOutput {
				return writeStatsJSON(cmd.OutOrStdout(), period, report)
			}
			return writeStatsText(cmd.OutOrStdout(), period, group, report)
		},
	}
	command.Flags().String("period", "all", "Period to report: today, week, month, or all")
	command.Flags().String("group-by", "", "Group results by day, week, or month")
	command.Flags().Bool("json", false, "Output stats as JSON")
	return command
}

func resolveStatsPeriod(name string, now time.Time) (statsPeriod, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	location := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	switch name {
	case "today", "day":
		return statsPeriod{name: "today", label: "today", from: today, to: today.AddDate(0, 0, 1)}, nil
	case "week":
		weekdayOffset := (int(today.Weekday()) + 6) % 7
		from := today.AddDate(0, 0, -weekdayOffset)
		return statsPeriod{name: name, label: "this week", from: from, to: from.AddDate(0, 0, 7)}, nil
	case "month":
		from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
		return statsPeriod{name: name, label: "this month", from: from, to: from.AddDate(0, 1, 0)}, nil
	case "all":
		return statsPeriod{name: name, label: "all time"}, nil
	default:
		return statsPeriod{}, fmt.Errorf("unsupported period %q: use today, week, month, or all", name)
	}
}

func parseStatsGroup(name string) (tldw.StatsGroup, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "none":
		return tldw.StatsGroupNone, nil
	case "day":
		return tldw.StatsGroupDay, nil
	case "week":
		return tldw.StatsGroupWeek, nil
	case "month":
		return tldw.StatsGroupMonth, nil
	default:
		return tldw.StatsGroupNone, fmt.Errorf("unsupported grouping %q: use day, week, or month", name)
	}
}

func writeStatsJSON(writer io.Writer, period statsPeriod, report tldw.StatsReport) error {
	output := statsJSONOutput{
		Period: period.name, VideoCount: report.VideoCount, DurationSeconds: report.DurationSeconds,
		EstimatedTimeSavedSeconds: report.DurationSeconds, Groups: report.Groups,
	}
	if !period.from.IsZero() {
		output.From = period.from.Format(time.RFC3339)
	}
	if !period.to.IsZero() {
		output.To = period.to.Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding stats: %w", err)
	}
	_, err = fmt.Fprintln(writer, string(data))
	return err
}

func writeStatsText(writer io.Writer, period statsPeriod, group tldw.StatsGroup, report tldw.StatsReport) error {
	duration := formatStatsDuration(report.DurationSeconds)
	if _, err := fmt.Fprintf(writer, "TLDW stats — %s\n", period.label); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%d unique %s\n", report.VideoCount, plural(report.VideoCount, "video", "videos")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Video runtime processed: %s\n", duration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Estimated watch time avoided: about %s\n", duration); err != nil {
		return err
	}
	if group == tldw.StatsGroupNone || len(report.Groups) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(writer, "\nBy %s:\n", group); err != nil {
		return err
	}
	for _, bucket := range report.Groups {
		if _, err := fmt.Fprintf(writer, "%s  %d %s  %s\n", bucket.Label, bucket.VideoCount,
			plural(bucket.VideoCount, "video", "videos"), formatStatsDuration(bucket.DurationSeconds)); err != nil {
			return err
		}
	}
	return nil
}

func formatStatsDuration(seconds float64) string {
	total := int(math.Round(seconds))
	if total < 0 {
		total = 0
	}
	hours := total / 3600
	minutes := (total % 3600) / 60
	remainingSeconds := total % 60
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if minutes > 0 {
		if remainingSeconds > 0 {
			return fmt.Sprintf("%dm %ds", minutes, remainingSeconds)
		}
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%ds", remainingSeconds)
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

var statsCmd = newStatsCommand(func() (statsApplication, error) {
	return newEngine(config)
}, time.Now)

func init() {
	rootCmd.AddCommand(statsCmd)
}
