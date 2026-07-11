// Package process provides the shared external process execution adapter.
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type CommandRunner struct{}

// CommandError preserves the command context needed to diagnose a failed
// external process while still supporting errors.Is and errors.As.
type CommandError struct {
	Name   string
	Args   []string
	Stderr string
	Err    error
}

func (err *CommandError) Error() string {
	message := fmt.Sprintf("%s failed: %v", err.Name, err.Err)
	if stderr := strings.TrimSpace(err.Stderr); stderr != "" {
		message += ": " + stderr
	}
	return message
}

func (err *CommandError) Unwrap() error { return err.Err }

func (*CommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), commandError(ctx, name, args, stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

func commandError(ctx context.Context, name string, args []string, stderr string, err error) *CommandError {
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = errors.Join(err, ctxErr)
	}
	return &CommandError{Name: name, Args: append([]string(nil), args...), Stderr: stderr, Err: err}
}
