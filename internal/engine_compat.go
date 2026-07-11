package internal

import "github.com/rtzll/tldw/internal/tldw"

type Engine = tldw.Engine
type EngineOption = tldw.EngineOption
type TranscriptPolicy = tldw.TranscriptPolicy
type TranscriptRequest = tldw.TranscriptRequest
type VideoAdapter = tldw.VideoAdapter
type AIAdapter = tldw.AIAdapter
type VideoStore = tldw.VideoStore
type LogSink = tldw.LogSink
type Summary = tldw.Summary
type PlaylistSummaryRequest = tldw.PlaylistSummaryRequest
type PlaylistSummaryResult = tldw.PlaylistSummaryResult
type VideoTranscript = tldw.VideoTranscript

const (
	TranscriptPolicyCaptionsOnly        = tldw.TranscriptPolicyCaptionsOnly
	TranscriptPolicyCaptionsThenWhisper = tldw.TranscriptPolicyCaptionsThenWhisper
	TranscriptPolicyWhisperOnly         = tldw.TranscriptPolicyWhisperOnly
	TranscriptPolicyWhisperAllowed      = tldw.TranscriptPolicyWhisperAllowed
)

var ErrCaptionsUnavailable = tldw.ErrCaptionsUnavailable

func NewEngine(config *Config, options ...EngineOption) *Engine {
	return tldw.NewEngine(tldw.Config{
		WhisperTimeout:       config.WhisperTimeout,
		MetadataCacheVersion: currentMetadataCacheVersion,
	}, NewPromptManager(config.ConfigDir, config.Prompt), options...)
}

func WithVideoAdapter(video VideoAdapter) EngineOption { return tldw.WithVideoAdapter(video) }
func WithAIAdapter(ai AIAdapter) EngineOption          { return tldw.WithAIAdapter(ai) }
func WithVideoStore(store VideoStore) EngineOption     { return tldw.WithVideoStore(store) }
func WithLogSink(log LogSink) EngineOption             { return tldw.WithLogSink(log) }

func WithYouTube(youtube *YouTube) EngineOption { return WithVideoAdapter(youtube) }
func WithAI(ai *AI) EngineOption                { return WithAIAdapter(ai) }
