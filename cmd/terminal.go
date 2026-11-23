package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

var errConsentRequired = errors.New("paid transcription requires confirmation; rerun with --allow-paid-transcription to authorize it")
var errConsentDeclined = errors.New("transcription declined")

func isTerminal(stream any) bool {
	f, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func canPrompt(input io.Reader, diagnostic io.Writer, nonInteractive bool) bool {
	ci := strings.ToLower(strings.TrimSpace(os.Getenv("CI")))
	return !nonInteractive && (ci == "" || ci == "false" || ci == "0") && isTerminal(input) && isTerminal(diagnostic)
}

// One scanner per invocation preserves buffered answers across playlist items.
func confirmationPrompt(ctx context.Context, input io.Reader, diagnostic io.Writer, interactive bool) func(string) (bool, error) {
	scanner := bufio.NewScanner(input)
	return func(message string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !interactive {
			return false, errConsentRequired
		}
		if _, err := fmt.Fprintf(diagnostic, "%s (y/N): ", message); err != nil {
			return false, err
		}
		// Terminal reads can block until the next line even after a signal.
		read := make(chan bool, 1)
		go func() { read <- scanner.Scan() }()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case ok := <-read:
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if !ok {
				return false, scanner.Err()
			}
			answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
			return answer == "y" || answer == "yes", nil
		}
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
