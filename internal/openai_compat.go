package internal

import (
	"time"

	openaiadapter "github.com/rtzll/tldw/internal/openai"
	"github.com/rtzll/tldw/internal/process"
)

type Audio = openaiadapter.Audio
type AI = openaiadapter.AI
type OpenAIClient = openaiadapter.OpenAIClient
type OpenAIClientInterface = openaiadapter.OpenAIClientInterface

func NewAudio(runner process.Runner, tempDir string, verbose bool) *Audio {
	return openaiadapter.NewAudio(runner, tempDir, verbose)
}

func NewOpenAIClient(apiKey string) *OpenAIClient {
	return openaiadapter.NewOpenAIClient(apiKey)
}

func NewAI(client OpenAIClientInterface, audio *Audio, model string, whisperLimit int64, timeout time.Duration, verbose, quiet bool) *AI {
	return openaiadapter.NewAI(client, audio, model, whisperLimit, timeout, verbose, quiet)
}

func NewAIWithKey(apiKey string, audio *Audio, model string, whisperLimit int64, timeout time.Duration, verbose, quiet bool) *AI {
	return openaiadapter.NewAIWithKey(apiKey, audio, model, whisperLimit, timeout, verbose, quiet)
}
