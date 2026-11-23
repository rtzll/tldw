package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Keep one scanner per invocation so a playlist consumes one answer per prompt.
func confirmationPrompt(input io.Reader) func(string) bool {
	scanner := bufio.NewScanner(input)
	return func(message string) bool {
		fmt.Printf("%s (y/N): ", message)
		ok := scanner.Scan()
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			return false
		}
		if !ok {
			return false
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
}

func renderMarkdown(content string) (string, error) {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width = 80
	} else if width > 10 {
		width -= 4
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
		glamour.WithColorProfile(termenv.EnvColorProfile()),
	)
	if err != nil {
		return "", fmt.Errorf("creating terminal renderer: %w", err)
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}
	return rendered, nil
}
