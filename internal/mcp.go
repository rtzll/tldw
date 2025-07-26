package internal

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps the MCP server and application dependencies
type MCPServer struct {
	app       *App
	mcpServer *server.MCPServer
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(app *App) *MCPServer {
	mcpServer := server.NewMCPServer(
		"tldw-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	s := &MCPServer{
		app:       app,
		mcpServer: mcpServer,
	}

	// Register tools
	s.registerTools()

	return s
}

// registerTools registers all available MCP tools
func (s *MCPServer) registerTools() {
	// get_youtube_metadata tool
	s.mcpServer.AddTool(mcp.NewTool("get_youtube_metadata",
		mcp.WithDescription("Extract video or playlist metadata including caption availability. For playlists, returns metadata for all videos. Check 'Has Captions' field to determine which transcript tool to use: if true, use get_youtube_transcript (free); if false, consider transcribe_youtube_whisper (paid)."),
		mcp.WithString("url",
			mcp.Description("YouTube video or playlist URL"),
			mcp.Required(),
		),
	), s.handleGetMetadata)

	// get_youtube_transcript tool (free - existing captions only)
	s.mcpServer.AddTool(mcp.NewTool("get_youtube_transcript",
		mcp.WithDescription("Get existing YouTube captions/transcript (FREE). For playlists, returns combined transcript of all videos. Only works if videos have captions - check metadata first. Fails if no captions available."),
		mcp.WithString("url",
			mcp.Description("YouTube video or playlist URL"),
			mcp.Required(),
		),
	), s.handleGetTranscript)

	// transcribe_youtube_whisper tool (paid - creates transcript using AI)
	s.mcpServer.AddTool(mcp.NewTool("transcribe_youtube_whisper",
		mcp.WithDescription("Create transcript using OpenAI Whisper API (PAID). For playlists, transcribes all videos - costs multiply by number of videos. Requires OPENAI_API_KEY environment variable to be set. Use only when videos have no captions and user explicitly agrees to incur costs. Always ask user for confirmation before calling this tool."),
		mcp.WithString("url",
			mcp.Description("YouTube video or playlist URL"),
			mcp.Required(),
		),
	), s.handleWhisperTranscribe)
}

// handleGetMetadata implements the get_youtube_metadata tool
func (s *MCPServer) handleGetMetadata(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract URL from arguments
	url, err := request.RequireString("url")
	if err != nil {
		return mcp.NewToolResultError("url parameter is required and must be a string"), nil
	}

	// Get metadata from YouTube
	metadata, err := s.app.youtube.Metadata(ctx, url)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("metadata error", err), nil
	}

	// Format metadata as text
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("Title: %s\n", metadata.Title))
	buf.WriteString(fmt.Sprintf("Channel: %s\n", metadata.Channel))
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

	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(buf.String())},
	}, nil
}

// handleGetTranscript implements the get_youtube_transcript tool (free captions only)
func (s *MCPServer) handleGetTranscript(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract URL from arguments
	url, err := request.RequireString("url")
	if err != nil {
		return mcp.NewToolResultError("url parameter is required and must be a string"), nil
	}

	// Try to get transcript from YouTube captions only (no Whisper fallback)
	transcript, err := s.app.GetTranscript(ctx, url)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("no captions available - use get_youtube_metadata to check caption availability, or consider transcribe_youtube_whisper (paid)", err), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(transcript)},
	}, nil
}

// handleWhisperTranscribe implements the transcribe_youtube_whisper tool (paid Whisper transcription)
func (s *MCPServer) handleWhisperTranscribe(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract URL from arguments
	url, err := request.RequireString("url")
	if err != nil {
		return mcp.NewToolResultError("url parameter is required and must be a string"), nil
	}

	// Download audio and transcribe using Whisper (this costs money)
	audioFile, err := s.app.DownloadAudio(ctx, url)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("failed to download audio", err), nil
	}

	transcript, err := s.app.TranscribeAudio(ctx, audioFile)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("failed to transcribe audio with Whisper", err), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(transcript)},
	}, nil
}

// Start starts the MCP server using the specified transport
func (s *MCPServer) Start(ctx context.Context, transport string, port int) error {
	if transport == "http" {
		httpServer := server.NewStreamableHTTPServer(s.mcpServer)
		addr := fmt.Sprintf(":%d", port)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return httpServer.Start(addr)
	}

	// Default to stdio transport
	return server.ServeStdio(s.mcpServer)
}

// GetServer returns the underlying MCP server for advanced configuration
func (s *MCPServer) GetServer() *server.MCPServer {
	return s.mcpServer
}
