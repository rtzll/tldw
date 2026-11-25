package ytdlp

import (
	"context"
	"io"
)

type mockCommandRunner struct {
	output []byte
	err    error
}

func (m *mockCommandRunner) RunWithInput(context.Context, io.Reader, string, ...string) ([]byte, error) {
	return m.output, m.err
}

type commandRunnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f commandRunnerFunc) RunWithInput(ctx context.Context, _ io.Reader, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}
