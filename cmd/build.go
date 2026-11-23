package cmd

import (
	"fmt"
	"io"

	"github.com/rtzll/tldw/internal"
	openaiadapter "github.com/rtzll/tldw/internal/openai"
	"github.com/rtzll/tldw/internal/process"
	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
	ytdlpadapter "github.com/rtzll/tldw/internal/ytdlp"
)

type cliLogSink struct {
	config     *internal.Config
	diagnostic io.Writer
}

func (l cliLogSink) Printf(format string, args ...any) {
	if l.config.Verbose && !l.config.Quiet {
		fmt.Fprintf(l.diagnostic, format, args...)
	}
}

func newEngine(config *internal.Config, diagnostic io.Writer) (*tldw.Engine, error) {
	return buildEngine(config, cliLogSink{config: config, diagnostic: diagnostic})
}

func buildEngine(config *internal.Config, log tldw.LogSink) (*tldw.Engine, error) {
	runner := &process.CommandRunner{}
	audio := openaiadapter.NewAudio(runner, config.TempDir)
	video := ytdlpadapter.New(config.TranscriptsDir, config.CacheDir, config.Verbose, config.Quiet)
	ai, err := openaiadapter.NewAIWithKey(config.OpenAIAPIKey, audio, openaiadapter.Config{
		Model: config.TLDRModel, TranscriptionUploadLimit: internal.TranscriptionUploadLimit, Timeout: config.SummaryTimeout,
		Verbose: config.Verbose, Quiet: config.Quiet,
	})
	if err != nil {
		return nil, fmt.Errorf("configuring OpenAI adapter: %w", err)
	}
	video.SetLogSink(log)
	ai.SetLogSink(log)
	return tldw.NewEngine(
		tldw.Config{
			TranscriptionTimeout: config.TranscriptionTimeout,
		},
		tldw.Dependencies{
			Video:   video,
			Store:   store.NewFile(config.TranscriptsDir),
			AI:      ai,
			Prompts: internal.NewPromptManager(config.ConfigDir, config.Prompt),
			Log:     log,
		},
	)
}
