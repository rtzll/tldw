package cmd

import (
	"fmt"

	"github.com/rtzll/tldw/internal"
	"github.com/rtzll/tldw/internal/store"
)

type cliLogSink struct {
	config *internal.Config
}

func (l cliLogSink) Printf(format string, args ...any) {
	if l.config.Verbose && !l.config.Quiet {
		fmt.Printf(format, args...)
	}
}

func newEngine(config *internal.Config) *internal.Engine {
	return buildEngine(config, cliLogSink{config: config})
}

type silentLogSink struct{}

func (silentLogSink) Printf(string, ...any) {}

func newMCPEngine(config *internal.Config) *internal.Engine {
	return buildEngine(config, silentLogSink{})
}

func buildEngine(config *internal.Config, log internal.LogSink) *internal.Engine {
	runner := &internal.DefaultCommandRunner{}
	audio := internal.NewAudio(runner, config.TempDir, config.Verbose)
	youtube := internal.NewYouTubeWithCache(config.TranscriptsDir, config.CacheDir, config.Verbose, config.Quiet)
	ai := internal.NewAIWithKey(config.OpenAIAPIKey, audio, config.TLDRModel, internal.WhisperLimit, config.SummaryTimeout, config.Verbose, config.Quiet)
	youtube.SetLogSink(log)
	ai.SetLogSink(log)
	return internal.NewEngine(
		config,
		internal.WithVideoAdapter(youtube),
		internal.WithVideoStore(store.NewFile(config.TranscriptsDir)),
		internal.WithAIAdapter(ai),
		internal.WithLogSink(log),
	)
}
