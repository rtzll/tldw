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

const mcpServerVersion = "1.0.0"

// NewMCPServer creates a new MCP server instance
func NewMCPServer(app *App) *MCPServer {
	InitMCPLogging(app.config)
	MCPLogInfo("Initializing MCP server (tldw-server v%s)", mcpServerVersion)

	mcpServer := server.NewMCPServer(
		"tldw-server",
		mcpServerVersion,
		server.WithToolCapabilities(true),
	)

	s := &MCPServer{
		app:       app,
		mcpServer: mcpServer,
	}

	s.registerTools()
	MCPLogInfo("MCP server initialized with %d tools", 3)
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
		MCPLogError("Tool: get_youtube_metadata - missing or invalid URL parameter")
		return mcp.NewToolResultError("url parameter is required and must be a string"), nil
	}

	MCPLogInfo("Tool: get_youtube_metadata - URL: %s", url)

	// Get metadata from YouTube
	metadata, err := s.app.youtube.Metadata(ctx, url)
	if err != nil {
		MCPLogError("Tool: get_youtube_metadata failed - %v", err)
		return mcp.NewToolResultErrorFromErr("metadata error", err), nil
	}

	MCPLogInfo("Tool: get_youtube_metadata succeeded - Title: %s, Duration: %.0fs, HasCaptions: %t",
		metadata.Title, metadata.Duration, metadata.HasCaptions)

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
		MCPLogError("Tool: get_youtube_transcript - missing or invalid URL parameter")
		return mcp.NewToolResultError("url parameter is required and must be a string"), nil
	}

	MCPLogInfo("Tool: get_youtube_transcript - URL: %s", url)

	// Try to get transcript from YouTube captions only (no Whisper fallback)
	transcript, err := s.app.GetTranscript(ctx, url)
	if err != nil {
		MCPLogError("Tool: get_youtube_transcript failed - %v", err)
		return mcp.NewToolResultErrorFromErr("no captions available - use get_youtube_metadata to check caption availability, or consider transcribe_youtube_whisper (paid)", err), nil
	}

	MCPLogInfo("Tool: get_youtube_transcript succeeded - transcript length: %d characters", len(transcript))

	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(transcript)},
	}, nil
}

// handleWhisperTranscribe implements the transcribe_youtube_whisper tool (paid Whisper transcription)
func (s *MCPServer) handleWhisperTranscribe(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract URL from arguments
	url, err := request.RequireString("url")
	if err != nil {
		MCPLogError("Tool: transcribe_youtube_whisper - missing or invalid URL parameter")
		return mcp.NewToolResultError("url parameter is required and must be a string"), nil
	}

	MCPLogInfo("Tool: transcribe_youtube_whisper - URL: %s (PAID OPERATION)", url)

	// Download audio and transcribe using Whisper (this costs money)
	audioFile, err := s.app.DownloadAudio(ctx, url)
	if err != nil {
		MCPLogError("Tool: transcribe_youtube_whisper - audio download failed: %v", err)
		return mcp.NewToolResultErrorFromErr("failed to download audio", err), nil
	}

	MCPLogInfo("Tool: transcribe_youtube_whisper - audio downloaded, starting transcription")

	transcript, err := s.app.TranscribeAudio(ctx, audioFile)
	if err != nil {
		MCPLogError("Tool: transcribe_youtube_whisper - transcription failed: %v", err)
		return mcp.NewToolResultErrorFromErr("failed to transcribe audio with Whisper", err), nil
	}

	MCPLogInfo("Tool: transcribe_youtube_whisper succeeded - transcript length: %d characters", len(transcript))

	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(transcript)},
	}, nil
}

// Start starts the MCP server using the specified transport
func (s *MCPServer) Start(ctx context.Context, transport string, port int) error {
	if transport == "http" {
		MCPLogInfo("Starting MCP server with HTTP transport on port %d", port)
		httpServer := server.NewStreamableHTTPServer(s.mcpServer)
		addr := fmt.Sprintf(":%d", port)
		if ctx.Err() != nil {
			MCPLogError("Context cancelled before HTTP server start")
			return ctx.Err()
		}
		err := httpServer.Start(addr)
		if err != nil {
			MCPLogError("HTTP server failed to start: %v", err)
		}
		return err
	}

	// Default to stdio transport
	MCPLogInfo("Starting MCP server with stdio transport")
	err := server.ServeStdio(s.mcpServer)
	if err != nil {
		MCPLogError("Stdio server failed: %v", err)
	}
	return err
}

// GetServer returns the underlying MCP server for advanced configuration
func (s *MCPServer) GetServer() *server.MCPServer {
	return s.mcpServer
}
