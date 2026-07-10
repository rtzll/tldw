package internal

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"sync"
)

// CommandExecutor is the single process-execution seam for buffered and
// streaming commands.
type CommandExecutor interface {
	CommandRunner
	RunStreaming(ctx context.Context, name string, args []string, onLine func(string)) error
}

func (r *DefaultCommandRunner) RunStreaming(ctx context.Context, name string, args []string, onLine func(string)) error {
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
	scan := func(scanner *bufio.Scanner) {
		defer wg.Done()
		for scanner.Scan() {
			callbackMu.Lock()
			onLine(scanner.Text())
			callbackMu.Unlock()
		}
	}
	wg.Add(2)
	go scan(bufio.NewScanner(stdout))
	go scan(bufio.NewScanner(stderr))

	waitErr := cmd.Wait()
	wg.Wait()
	return waitErr
}
