package cmd

import (
	"fmt"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"

	"github.com/spf13/cobra"
)

func parseSummaryReference(input string, commands []*cobra.Command) (tldw.ContentRef, error) {
	ref, err := tldw.ParseReference(input)
	if err == nil && (ref.IsVideo() || ref.IsPlaylist()) {
		return ref, nil
	}
	if suggestion, ok := commandSuggestion(input, commands); ok {
		return tldw.ContentRef{}, fmt.Errorf("invalid summary input %q: %s", input, suggestion)
	}
	if err != nil {
		return tldw.ContentRef{}, fmt.Errorf("invalid input %q: %w", input, err)
	}
	return tldw.ContentRef{}, fmt.Errorf("summaries require a video or playlist, got %s", ref.Kind())
}

func commandSuggestion(input string, commands []*cobra.Command) (string, bool) {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" || strings.HasPrefix(input, "@") || strings.Contains(input, "://") {
		return "", false
	}
	commandNames := []string{"help"}
	for _, command := range commands {
		commandNames = append(commandNames, command.Name())
	}
	for _, name := range commandNames {
		if strings.Contains(name, input) || strings.Contains(input, name) {
			return "did you mean: " + name, true
		}
	}
	return "", false
}
