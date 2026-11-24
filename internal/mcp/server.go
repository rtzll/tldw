package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rtzll/tldw/internal/telemetry"
	"github.com/rtzll/tldw/internal/tldw"
)

// MCPServer wraps the MCP server and application dependencies
type MCPServer struct {
	engine MCPApplication
	log    *slog.Logger
	tracer trace.Tracer
}

type MCPApplication interface {
	MetadataFor(context.Context, tldw.ContentRef) (*tldw.VideoMetadata, error)
	Transcript(context.Context, tldw.ContentRef, tldw.TranscriptRequest) (*tldw.Transcript, error)
}

const (
	mcpServerVersion  = "1.0.0"
	mcpMethodCallTool = "tools/call"

	mcpGetMetadataDescription       = "Check metadata and caption availability for a YouTube video or single-video X post. Use get_youtube_transcript for free captions. If captions are unavailable, offer paid OpenAI transcription."
	mcpGetTranscriptDescription     = "Get existing captions for a YouTube video or single-video X post (FREE)."
	mcpPaidTranscriptionDescription = "Transcribe a YouTube video or single-video X post using OpenAI's gpt-transcribe model (PAID). Requires OPENAI_API_KEY. Use only when captions are unavailable and the user explicitly approves the cost."
)

type mcpGetMetadataInput struct {
	URL string `json:"url" jsonschema:"YouTube video URL or single-video X post URL"`
}

type mcpGetTranscriptInput struct {
	URL               string `json:"url" jsonschema:"YouTube video URL or single-video X post URL"`
	IncludeTimestamps bool   `json:"include_timestamps,omitempty" jsonschema:"When true, return transcript lines with timestamps if caption timing data is available."`
}

type mcpPaidTranscriptionInput struct {
	URL               string `json:"url" jsonschema:"YouTube video URL or single-video X post URL"`
	IncludeTimestamps bool   `json:"include_timestamps,omitempty" jsonschema:"Omit or set to false. Paid transcription returns plain text."`
}

type mcpChapterOutput struct {
	StartTime float64 `json:"start_time" jsonschema:"Video chapter start time in seconds"`
	EndTime   float64 `json:"end_time" jsonschema:"Video chapter end time in seconds"`
	Title     string  `json:"title" jsonschema:"Video chapter title"`
}

type mcpMetadataOutput struct {
	Title            string             `json:"title" jsonschema:"Video title"`
	Channel          string             `json:"channel" jsonschema:"Upload channel or X account name"`
	Creators         []string           `json:"creators,omitempty" jsonschema:"Creators or collaborators associated with the video"`
	DurationSeconds  float64            `json:"duration_seconds" jsonschema:"Duration in seconds"`
	Description      string             `json:"description" jsonschema:"Video description"`
	Language         string             `json:"language,omitempty" jsonschema:"Detected video language"`
	HasCaptions      bool               `json:"has_captions" jsonschema:"Whether captions are available"`
	CaptionLanguages []string           `json:"caption_languages,omitempty" jsonschema:"Available caption language codes"`
	Tags             []string           `json:"tags,omitempty" jsonschema:"Video tags"`
	Categories       []string           `json:"categories,omitempty" jsonschema:"Video categories"`
	Chapters         []mcpChapterOutput `json:"chapters,omitempty" jsonschema:"Video chapters"`
}

type mcpTranscriptOutput struct {
	URL               string `json:"url" jsonschema:"Requested YouTube video URL or single-video X post URL"`
	Transcript        string `json:"transcript" jsonschema:"Transcript text"`
	Source            string `json:"source" jsonschema:"Transcript source (captions for existing captions or paid_transcription for paid OpenAI transcription)"`
	IncludeTimestamps bool   `json:"include_timestamps" jsonschema:"Whether timestamps were requested in the transcript text"`
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(engine MCPApplication, logger *slog.Logger, tracer trace.Tracer) *MCPServer {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("mcp")
	}
	return &MCPServer{engine: engine, log: logger, tracer: tracer}
}

// Each transport owns its protocol server and its middleware.
func (s *MCPServer) protocolServer(transport string) *mcp.Server {
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "tldw-server",
		Version: mcpServerVersion,
	}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{ListChanged: true},
		},
	})

	if transport == "stdio" {
		mcpServer.AddReceivingMiddleware(serializeStdioToolCalls)
	}
	mcpServer.AddReceivingMiddleware(traceToolCalls(s.tracer, transport))
	s.registerTools(mcpServer)
	return mcpServer
}

func serializeStdioToolCalls(next mcp.MethodHandler) mcp.MethodHandler {
	token := make(chan struct{}, 1)
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		if method != mcpMethodCallTool {
			return next(ctx, method, request)
		}

		_, wait := telemetry.Start(ctx, "mcp.stdio.wait")
		select {
		case token <- struct{}{}:
			defer func() { <-token }()
			tldw.EndOperation(wait, ctx.Err())
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		case <-ctx.Done():
			tldw.EndOperation(wait, ctx.Err())
			return nil, ctx.Err()
		}

		return next(ctx, method, request)
	}
}

// registerTools registers all available MCP tools
func (s *MCPServer) registerTools(server *mcp.Server) {
	// get_youtube_metadata tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_youtube_metadata",
		Description: mcpGetMetadataDescription,
		Annotations: mcpToolAnnotations(true),
	}, s.handleGetMetadata)

	// get_youtube_transcript tool (free - existing captions only)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_youtube_transcript",
		Description: mcpGetTranscriptDescription,
		Annotations: mcpToolAnnotations(true),
	}, s.handleGetTranscript)

	// transcribe_video_paid tool (paid - creates transcript using AI)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "transcribe_video_paid",
		Description: mcpPaidTranscriptionDescription,
		Annotations: mcpToolAnnotations(false),
	}, s.handlePaidTranscription)
}

func mcpToolAnnotations(readOnly bool) *mcp.ToolAnnotations {
	destructive := false
	openWorld := true
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    readOnly,
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
	parsed, err := tldw.ParseVideoRef(input.URL)
	if err != nil {
		return nil, zero, fmt.Errorf("invalid video URL: %w", err)
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("tldw.video.id", parsed.CacheKey()))

	// Get video metadata
	metadata, err := s.engine.MetadataFor(ctx, parsed)
	if err != nil {
		return nil, zero, fmt.Errorf("metadata error: %w", err)
	}

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
		output.Chapters = append(output.Chapters, mcpChapterOutput(ch))
	}

	// Format metadata as text
	var buf strings.Builder
	fmt.Fprintf(&buf, "Title: %s\n", metadata.Title)
	fmt.Fprintf(&buf, "Channel: %s\n", metadata.Channel)
	if len(metadata.Creators) > 0 {
		fmt.Fprintf(&buf, "Creators: %s\n", strings.Join(metadata.Creators, ", "))
	}
	fmt.Fprintf(&buf, "Duration: %.0f seconds\n", metadata.Duration)
	fmt.Fprintf(&buf, "Description: %s\n", metadata.Description)

	// Caption availability information
	fmt.Fprintf(&buf, "Has Captions: %t\n", metadata.HasCaptions)

	if len(metadata.Tags) > 0 {
		fmt.Fprintf(&buf, "Tags: %s\n", strings.Join(metadata.Tags, ", "))
	}

	if len(metadata.Categories) > 0 {
		fmt.Fprintf(&buf, "Categories: %s\n", strings.Join(metadata.Categories, ", "))
	}

	for _, ch := range metadata.Chapters {
		fmt.Fprintf(&buf, "Chapter (%.0f–%.0f): %s\n", ch.StartTime, ch.EndTime, ch.Title)
	}

	return mcpTextResult(buf.String()), output, nil
}

// handleGetTranscript implements the get_youtube_transcript tool (free captions only)
func (s *MCPServer) handleGetTranscript(ctx context.Context, _ *mcp.CallToolRequest, input mcpGetTranscriptInput) (*mcp.CallToolResult, mcpTranscriptOutput, error) {
	var zero mcpTranscriptOutput
	parsed, err := tldw.ParseVideoRef(input.URL)
	if err != nil {
		return nil, zero, fmt.Errorf("invalid video URL: %w", err)
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("tldw.video.id", parsed.CacheKey()))
	includeTimestamps := input.IncludeTimestamps

	format := tldw.TranscriptRenderFormatPlain
	if includeTimestamps {
		format = tldw.TranscriptRenderFormatTimestamps
	}

	request := tldw.CaptionsOnly
	if includeTimestamps {
		request = tldw.TimestampedCaptions
	}
	structured, err := s.engine.Transcript(ctx, parsed, request)
	if err != nil {
		if errors.Is(err, tldw.ErrCaptionsUnavailable) {
			return nil, zero, fmt.Errorf("%w. Offer transcribe_video_paid for a plain-text transcript (paid) and ask for confirmation first", err)
		}
		return nil, zero, fmt.Errorf("getting transcript: %w", err)
	}
	transcript, err := structured.Render(format)
	if err != nil {
		return nil, zero, err
	}

	output := mcpTranscriptOutput{
		URL:               parsed.URL(),
		Transcript:        transcript,
		Source:            string(tldw.TranscriptSourceCaptions),
		IncludeTimestamps: includeTimestamps,
	}

	return mcpTextResult(transcript), output, nil
}

// handlePaidTranscription implements the transcribe_video_paid tool (paid OpenAI transcription)
func (s *MCPServer) handlePaidTranscription(ctx context.Context, _ *mcp.CallToolRequest, input mcpPaidTranscriptionInput) (*mcp.CallToolResult, mcpTranscriptOutput, error) {
	var zero mcpTranscriptOutput
	parsed, err := tldw.ParseVideoRef(input.URL)
	if err != nil {
		return nil, zero, fmt.Errorf("invalid video URL: %w", err)
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("tldw.video.id", parsed.CacheKey()))
	if input.IncludeTimestamps {
		err := fmt.Errorf("paid transcription returns plain text without timestamps")
		return nil, zero, err
	}

	structured, err := s.engine.Transcript(ctx, parsed, tldw.PaidOnly)
	if err != nil {
		return nil, zero, fmt.Errorf("failed to transcribe audio with OpenAI: %w", err)
	}
	transcript, err := structured.Render(tldw.TranscriptRenderFormatPlain)
	if err != nil {
		return nil, zero, err
	}

	output := mcpTranscriptOutput{
		URL:               parsed.URL(),
		Transcript:        transcript,
		Source:            string(tldw.TranscriptSourcePaidTranscription),
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
		s.log.Info("MCP server starting", "transport", transport, "address", addr)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		server := s.protocolServer(transport)
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return server
		}, &mcp.StreamableHTTPOptions{
			// MCP 2026-07-28 requires stateless HTTP. These tools do not
			// need persistent sessions or server-initiated client requests.
			Stateless:                    true,
			PropagateRequestCancellation: true,
		})
		// Route only MCP endpoints to the protocol handler. In particular,
		// unsupported OAuth discovery URLs must return 404 so tunnel clients
		// recognize this as a plain MCP server, not malformed OAuth metadata.
		mux := http.NewServeMux()
		mux.Handle("/mcp", handler)
		mux.Handle("/{$}", handler) // Preserve the legacy root endpoint.
		httpServer := &http.Server{
			Addr:        addr,
			Handler:     traceHTTP(s.tracer, mux),
			BaseContext: func(net.Listener) context.Context { return ctx },
		}
		return serveHTTP(ctx, httpServer)
	case "stdio":
		s.log.Info("MCP server starting", "transport", transport)
		server := s.protocolServer(transport)
		err := server.Run(ctx, &mcp.StdioTransport{})
		return err
	default:
		return fmt.Errorf("invalid MCP transport %q; expected stdio or http", transport)
	}
}

func serveHTTP(ctx context.Context, server *http.Server) error {
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(drain); err != nil {
			if err := server.Close(); err != nil {
				return err
			}
		}
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return ctx.Err()
	}
}
