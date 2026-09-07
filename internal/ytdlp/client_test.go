package ytdlp

import "context"

type mockCommandRunner struct {
	output []byte
	err    error
}

func (m *mockCommandRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return m.output, m.err
}

type commandRunnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f commandRunnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}
