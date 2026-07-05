package internal

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPServer wraps the MCP server and application dependencies
type MCPServer struct {
	app                *App
	mcpServer          *mcp.Server
	stdioToolMu        sync.Mutex
	stdioSerializeOnce sync.Once
}

const (
	mcpServerVersion  = "1.0.0"
	mcpMethodCallTool = "tools/call"

	mcpGetMetadataDescription   = "Extract video metadata including caption availability. Check 'Has Captions' field to determine which transcript tool to use: if true, use get_youtube_transcript (free); if false, consider transcribe_youtube_whisper (paid)."
	mcpGetTranscriptDescription = "Get existing YouTube captions/transcript (FREE). Only works if the video has captions - check metadata first. Fails if no captions available."
	mcpWhisperDescription       = "Create transcript using OpenAI Whisper API (PAID). Requires OPENAI_API_KEY environment variable to be set. Use only when videos have no captions and user explicitly agrees to incur costs. Always ask user for confirmation before calling this tool."
)

type mcpGetMetadataInput struct {
	URL string `json:"url" jsonschema:"YouTube video URL"`
}

type mcpGetTranscriptInput struct {
	URL               string `json:"url" jsonschema:"YouTube video URL"`
	IncludeTimestamps bool   `json:"include_timestamps,omitempty" jsonschema:"When true, return transcript lines with timestamps if caption timing data is available."`
}

type mcpWhisperInput struct {
	URL               string `json:"url" jsonschema:"YouTube video URL"`
	IncludeTimestamps bool   `json:"include_timestamps,omitempty" jsonschema:"Reserved for future use. Timestamped Whisper transcripts are not supported yet."`
}

type mcpChapterOutput struct {
	StartTime float64 `json:"start_time" jsonschema:"Video chapter start time in seconds"`
	EndTime   float64 `json:"end_time" jsonschema:"Video chapter end time in seconds"`
	Title     string  `json:"title" jsonschema:"Video chapter title"`
}

type mcpMetadataOutput struct {
	Title            string             `json:"title" jsonschema:"YouTube video title"`
	Channel          string             `json:"channel" jsonschema:"Main YouTube upload channel name"`
	Creators         []string           `json:"creators,omitempty" jsonschema:"Creators or collaborators associated with the video"`
	DurationSeconds  float64            `json:"duration_seconds" jsonschema:"Duration in seconds"`
	Description      string             `json:"description" jsonschema:"YouTube description"`
	Language         string             `json:"language,omitempty" jsonschema:"Detected video language"`
	HasCaptions      bool               `json:"has_captions" jsonschema:"Whether captions are available"`
	CaptionLanguages []string           `json:"caption_languages,omitempty" jsonschema:"Available caption language codes"`
	Tags             []string           `json:"tags,omitempty" jsonschema:"YouTube video tags"`
	Categories       []string           `json:"categories,omitempty" jsonschema:"YouTube video categories"`
	Chapters         []mcpChapterOutput `json:"chapters,omitempty" jsonschema:"Video chapters"`
}

type mcpTranscriptOutput struct {
	URL               string `json:"url" jsonschema:"Requested YouTube video URL"`
	Transcript        string `json:"transcript" jsonschema:"Transcript text"`
	Source            string `json:"source" jsonschema:"Transcript source"`
	IncludeTimestamps bool   `json:"include_timestamps" jsonschema:"Whether timestamps were requested in the transcript text"`
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(app *App) *MCPServer {
	InitMCPLogging(app.config)
	MCPLogInfo("Initializing MCP server (tldw-server v%s)", mcpServerVersion)

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "tldw-server",
		Version: mcpServerVersion,
	}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{ListChanged: true},
		},
	})

	s := &MCPServer{
		app:       app,
		mcpServer: mcpServer,
	}

	s.registerTools()
	MCPLogInfo("MCP server initialized with %d tools", 3)
	return s
}

func (s *MCPServer) serializeStdioToolCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		if method != mcpMethodCallTool {
			return next(ctx, method, request)
		}

		s.stdioToolMu.Lock()
		defer s.stdioToolMu.Unlock()

		return next(ctx, method, request)
	}
}

// registerTools registers all available MCP tools
func (s *MCPServer) registerTools() {
	// get_youtube_metadata tool
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "get_youtube_metadata",
		Description: mcpGetMetadataDescription,
		Annotations: mcpToolAnnotations(),
	}, s.handleGetMetadata)

	// get_youtube_transcript tool (free - existing captions only)
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "get_youtube_transcript",
		Description: mcpGetTranscriptDescription,
		Annotations: mcpToolAnnotations(),
	}, s.handleGetTranscript)

	// transcribe_youtube_whisper tool (paid - creates transcript using AI)
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "transcribe_youtube_whisper",
		Description: mcpWhisperDescription,
		Annotations: mcpToolAnnotations(),
	}, s.handleWhisperTranscribe)
}

func mcpToolAnnotations() *mcp.ToolAnnotations {
	destructive := false
	openWorld := true
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: &destructive,
		OpenWorldHint:   &openWorld,
	}
}

func mcpTextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// handleGetMetadata implements the get_youtube_metadata tool
func (s *MCPServer) handleGetMetadata(ctx context.Context, _ *mcp.CallToolRequest, input mcpGetMetadataInput) (*mcp.CallToolResult, mcpMetadataOutput, error) {
	var zero mcpMetadataOutput
	url := input.URL
	parsed, err := ParseVideoArg(url)
	if err != nil {
		MCPLogError("Tool: get_youtube_metadata - invalid URL: %v", err)
		return nil, zero, fmt.Errorf("invalid YouTube video URL: %w", err)
	}
	url = parsed.NormalizedURL
	MCPLogInfo("Tool: get_youtube_metadata - URL: %s", url)

	// Get metadata from YouTube
	metadata, err := s.app.youtube.Metadata(ctx, url)
	if err != nil {
		MCPLogError("Tool: get_youtube_metadata failed - %v", err)
		return nil, zero, fmt.Errorf("metadata error: %w", err)
	}

	MCPLogInfo("Tool: get_youtube_metadata succeeded - Title: %s, Duration: %.0fs, HasCaptions: %t",
		metadata.Title, metadata.Duration, metadata.HasCaptions)

	output := mcpMetadataOutput{
		Title:            metadata.Title,
		Channel:          metadata.Channel,
		Creators:         metadata.Creators,
		DurationSeconds:  metadata.Duration,
		Description:      metadata.Description,
		Language:         metadata.Language,
		HasCaptions:      metadata.HasCaptions,
		CaptionLanguages: metadata.CaptionLanguages,
		Tags:             metadata.Tags,
		Categories:       metadata.Categories,
		Chapters:         make([]mcpChapterOutput, 0, len(metadata.Chapters)),
	}
	for _, ch := range metadata.Chapters {
		output.Chapters = append(output.Chapters, mcpChapterOutput{
			StartTime: ch.StartTime,
			EndTime:   ch.EndTime,
			Title:     ch.Title,
		})
	}

	// Format metadata as text
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("Title: %s\n", metadata.Title))
	buf.WriteString(fmt.Sprintf("Channel: %s\n", metadata.Channel))
	if len(metadata.Creators) > 0 {
		buf.WriteString(fmt.Sprintf("Creators: %s\n", strings.Join(metadata.Creators, ", ")))
	}
	buf.WriteString(fmt.Sprintf("Duration: %.0f seconds\n", metadata.Duration))
	buf.WriteString(fmt.Sprintf("Description: %s\n", metadata.Description))

	// Caption availability information
	buf.WriteString(fmt.Sprintf("Has Captions: %t\n", metadata.HasCaptions))

	if len(metadata.Tags) > 0 {
		buf.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(metadata.Tags, ", ")))
	}

	if len(metadata.Categories) > 0 {
		buf.WriteString(fmt.Sprintf("Categories: %s\n", strings.Join(metadata.Categories, ", ")))
	}

	for _, ch := range metadata.Chapters {
		buf.WriteString(fmt.Sprintf("Chapter (%.0f–%.0f): %s\n", ch.StartTime, ch.EndTime, ch.Title))
	}

	return mcpTextResult(buf.String()), output, nil
}

// handleGetTranscript implements the get_youtube_transcript tool (free captions only)
func (s *MCPServer) handleGetTranscript(ctx context.Context, _ *mcp.CallToolRequest, input mcpGetTranscriptInput) (*mcp.CallToolResult, mcpTranscriptOutput, error) {
	var zero mcpTranscriptOutput
	url := input.URL
	parsed, err := ParseVideoArg(url)
	if err != nil {
		MCPLogError("Tool: get_youtube_transcript - invalid URL: %v", err)
		return nil, zero, fmt.Errorf("invalid YouTube video URL: %w", err)
	}
	url = parsed.NormalizedURL
	MCPLogInfo("Tool: get_youtube_transcript - URL: %s", url)
	includeTimestamps := input.IncludeTimestamps

	format := TranscriptRenderFormatPlain
	if includeTimestamps {
		format = TranscriptRenderFormatTimestamps
	}

	// Try to get transcript from YouTube captions only (no Whisper fallback)
	transcript, err := s.app.GetTranscriptOutput(ctx, url, format)
	if err != nil {
		MCPLogError("Tool: get_youtube_transcript failed - %v", err)
		return nil, zero, fmt.Errorf("no captions available - use get_youtube_metadata to check caption availability, or consider transcribe_youtube_whisper (paid): %w", err)
	}

	MCPLogInfo("Tool: get_youtube_transcript succeeded - transcript length: %d characters", len(transcript))

	output := mcpTranscriptOutput{
		URL:               url,
		Transcript:        transcript,
		Source:            string(TranscriptSourceCaptions),
		IncludeTimestamps: includeTimestamps,
	}

	return mcpTextResult(transcript), output, nil
}

// handleWhisperTranscribe implements the transcribe_youtube_whisper tool (paid Whisper transcription)
func (s *MCPServer) handleWhisperTranscribe(ctx context.Context, _ *mcp.CallToolRequest, input mcpWhisperInput) (*mcp.CallToolResult, mcpTranscriptOutput, error) {
	var zero mcpTranscriptOutput
	url := input.URL
	parsed, err := ParseVideoArg(url)
	if err != nil {
		MCPLogError("Tool: transcribe_youtube_whisper - invalid URL: %v", err)
		return nil, zero, fmt.Errorf("invalid YouTube video URL: %w", err)
	}
	url = parsed.NormalizedURL
	MCPLogInfo("Tool: transcribe_youtube_whisper - URL: %s (PAID OPERATION)", url)
	if input.IncludeTimestamps {
		err := fmt.Errorf("timestamped Whisper transcripts are not supported yet")
		MCPLogError("Tool: transcribe_youtube_whisper failed - %v", err)
		return nil, zero, err
	}

	// Download audio and transcribe using Whisper (this costs money)
	audioFile, err := s.app.DownloadAudio(ctx, url)
	if err != nil {
		MCPLogError("Tool: transcribe_youtube_whisper - audio download failed: %v", err)
		return nil, zero, fmt.Errorf("failed to download audio: %w", err)
	}

	MCPLogInfo("Tool: transcribe_youtube_whisper - audio downloaded, starting transcription")

	transcript, err := s.app.TranscribeAudio(ctx, audioFile)
	if err != nil {
		MCPLogError("Tool: transcribe_youtube_whisper - transcription failed: %v", err)
		return nil, zero, fmt.Errorf("failed to transcribe audio with Whisper: %w", err)
	}

	MCPLogInfo("Tool: transcribe_youtube_whisper succeeded - transcript length: %d characters", len(transcript))

	output := mcpTranscriptOutput{
		URL:               url,
		Transcript:        transcript,
		Source:            string(TranscriptSourceWhisper),
		IncludeTimestamps: false,
	}

	return mcpTextResult(transcript), output, nil
}

// Start starts the MCP server using the specified transport
func (s *MCPServer) Start(ctx context.Context, transport, host string, port int) error {
	switch transport {
	case "http":
		if host == "" {
			host = "127.0.0.1"
		}

		addr := net.JoinHostPort(host, strconv.Itoa(port))
		MCPLogInfo("Starting MCP server with HTTP transport on %s", addr)
		if ctx.Err() != nil {
			MCPLogError("Context cancelled before HTTP server start")
			return ctx.Err()
		}

		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return s.mcpServer
		}, nil)
		httpServer := &http.Server{
			Addr:    addr,
			Handler: handler,
		}
		errCh := make(chan error, 1)
		go func() {
			errCh <- httpServer.ListenAndServe()
		}()

		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				MCPLogError("HTTP server shutdown failed: %v", err)
				return err
			}
			if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
				MCPLogError("HTTP server failed: %v", err)
				return err
			}
			return ctx.Err()
		case err := <-errCh:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			MCPLogError("HTTP server failed to start: %v", err)
			return err
		}
	case "stdio":
		MCPLogInfo("Starting MCP server with stdio transport")
		s.stdioSerializeOnce.Do(func() {
			s.mcpServer.AddReceivingMiddleware(s.serializeStdioToolCalls)
		})
		err := s.mcpServer.Run(ctx, &mcp.StdioTransport{})
		if err != nil {
			MCPLogError("Stdio server failed: %v", err)
		}
		return err
	default:
		return fmt.Errorf("invalid MCP transport %q; expected stdio or http", transport)
	}
}

// GetServer returns the underlying MCP server for advanced configuration
func (s *MCPServer) GetServer() *mcp.Server {
	return s.mcpServer
}
