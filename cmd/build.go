package cmd

import (
	"fmt"

	"github.com/rtzll/tldw/internal"
	openaiadapter "github.com/rtzll/tldw/internal/openai"
	"github.com/rtzll/tldw/internal/process"
	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

type cliLogSink struct {
	config *internal.Config
}

func (l cliLogSink) Printf(format string, args ...any) {
	if l.config.Verbose && !l.config.Quiet {
		fmt.Printf(format, args...)
	}
}

func newEngine(config *internal.Config) *tldw.Engine {
	return buildEngine(config, cliLogSink{config: config})
}

type silentLogSink struct{}

func (silentLogSink) Printf(string, ...any) {}

func newMCPEngine(config *internal.Config) *tldw.Engine {
	return buildEngine(config, silentLogSink{})
}

func buildEngine(config *internal.Config, log tldw.LogSink) *tldw.Engine {
	runner := &process.CommandRunner{}
	audio := openaiadapter.NewAudio(runner, config.TempDir, config.Verbose)
	youtube := internal.NewYouTubeWithCache(config.TranscriptsDir, config.CacheDir, config.Verbose, config.Quiet)
	ai := openaiadapter.NewAIWithKey(config.OpenAIAPIKey, audio, config.TLDRModel, internal.WhisperLimit, config.SummaryTimeout, config.Verbose, config.Quiet)
	youtube.SetLogSink(log)
	ai.SetLogSink(log)
	return tldw.NewEngine(
		tldw.Config{
			WhisperTimeout:       config.WhisperTimeout,
			MetadataCacheVersion: store.MetadataCacheVersion,
		},
		internal.NewPromptManager(config.ConfigDir, config.Prompt),
		tldw.WithVideoAdapter(youtube),
		tldw.WithVideoStore(store.NewFile(config.TranscriptsDir)),
		tldw.WithAIAdapter(ai),
		tldw.WithLogSink(log),
	)
}
