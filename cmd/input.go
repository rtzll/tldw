package cmd

import "strings"

var rootCommandNames = []string{"mcp", "transcribe", "cp", "version", "paths", "help"}

func commandSuggestion(input string) (string, bool) {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" || strings.HasPrefix(input, "@") || strings.Contains(input, "://") {
		return "", false
	}
	for _, command := range rootCommandNames {
		if strings.Contains(command, input) || strings.Contains(input, command) {
			return "did you mean: " + command, true
		}
	}
	return "", false
}
