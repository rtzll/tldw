package ytdlp

import "context"

type mockCommandRunner struct {
	output []byte
	err    error
}

func (m *mockCommandRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return m.output, m.err
}
