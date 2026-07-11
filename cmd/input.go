package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

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
