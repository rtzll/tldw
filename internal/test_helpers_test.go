package internal

import (
	"context"
	"os"
)

type mockCommandRunner struct {
	output []byte
	err    error
}

func (m *mockCommandRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return m.output, m.err
}

func (m *mockCommandRunner) RunStreaming(context.Context, string, []string, func(string)) error {
	return m.err
}

type mockOpenAIClient struct {
	transcription string
	chatResponse  string
	err           error
}

func (m *mockOpenAIClient) CreateTranscription(context.Context, *os.File) (string, error) {
	return m.transcription, m.err
}

func (m *mockOpenAIClient) CreateChatCompletion(context.Context, string, string) (string, error) {
	return m.chatResponse, m.err
}
