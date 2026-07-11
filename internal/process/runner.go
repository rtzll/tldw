// Package process provides the shared external process execution adapter.
package process

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type Executor interface {
	Runner
	RunStreaming(ctx context.Context, name string, args []string, onLine func(string)) error
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

func (*CommandRunner) RunStreaming(ctx context.Context, name string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("creating stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", name, err)
	}

	var wg sync.WaitGroup
	var callbackMu sync.Mutex
	var stderrBuffer bytes.Buffer
	readErrors := make(chan error, 2)
	read := func(streamName string, reader io.Reader) {
		defer wg.Done()
		if err := readLines(reader, func(line string) {
			callbackMu.Lock()
			onLine(line)
			callbackMu.Unlock()
		}); err != nil {
			readErrors <- fmt.Errorf("reading %s: %w", streamName, err)
		}
	}
	wg.Add(2)
	go read("stdout", stdout)
	go read("stderr", io.TeeReader(stderr, &stderrBuffer))
	waitErr := cmd.Wait()
	wg.Wait()
	close(readErrors)
	var readErr error
	for err := range readErrors {
		readErr = errors.Join(readErr, err)
	}
	if waitErr != nil {
		return errors.Join(commandError(ctx, name, args, stderrBuffer.String(), waitErr), readErr)
	}
	return readErr
}

func readLines(reader io.Reader, onLine func(string)) error {
	buffered := bufio.NewReader(reader)
	for {
		line, err := buffered.ReadString('\n')
		if line != "" {
			onLine(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func commandError(ctx context.Context, name string, args []string, stderr string, err error) *CommandError {
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = errors.Join(err, ctxErr)
	}
	return &CommandError{Name: name, Args: append([]string(nil), args...), Stderr: stderr, Err: err}
}
