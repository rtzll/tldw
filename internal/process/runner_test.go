package process_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	processadapter "github.com/rtzll/tldw/internal/process"
)

func TestCommandRunnerReturnsInspectableCommandError(t *testing.T) {
	t.Setenv("GO_WANT_PROCESS_HELPER", "1")
	runner := &processadapter.CommandRunner{}

	_, err := runner.Run(context.Background(), os.Args[0], "-test.run=TestProcessHelper", "--", "fail")
	var commandErr *processadapter.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("Run() error = %T %v, want *CommandError", err, err)
	}
	if commandErr.Name != os.Args[0] || !strings.Contains(commandErr.Stderr, "deliberate failure") {
		t.Fatalf("CommandError = %+v", commandErr)
	}
}

func TestCommandRunnerStreamsLongLines(t *testing.T) {
	t.Setenv("GO_WANT_PROCESS_HELPER", "1")
	runner := &processadapter.CommandRunner{}
	want := strings.Repeat("x", 128*1024)
	var lines []string

	err := runner.RunStreaming(context.Background(), os.Args[0], []string{"-test.run=TestProcessHelper", "--", "long-line"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("RunStreaming() error = %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("RunStreaming() delivered %d lines, want one", len(lines))
	}
	if lines[0] != want {
		t.Fatalf("RunStreaming() line length = %d, want %d", len(lines[0]), len(want))
	}
}

func TestProcessHelper(t *testing.T) {
	if os.Getenv("GO_WANT_PROCESS_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg != "--" || i+1 >= len(os.Args) {
			continue
		}
		switch os.Args[i+1] {
		case "fail":
			fmt.Fprintln(os.Stderr, "deliberate failure")
			os.Exit(3)
		case "long-line":
			fmt.Println(strings.Repeat("x", 128*1024))
			os.Exit(0)
		}
	}
	os.Exit(2)
}
