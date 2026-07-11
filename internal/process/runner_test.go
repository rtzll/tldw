package process_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

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

func TestCommandRunnerPreservesCancellationAndCommandFailure(t *testing.T) {
	t.Setenv("GO_WANT_PROCESS_HELPER", "1")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := (&processadapter.CommandRunner{}).Run(ctx, os.Args[0], "-test.run=TestProcessHelper", "--", "block")
	var commandErr *processadapter.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("Run() error = %T %v, want *CommandError", err, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run() error = %v, want preserved exec.ExitError", err)
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
		case "block":
			time.Sleep(5 * time.Second)
			os.Exit(0)
		}
	}
	os.Exit(2)
}
