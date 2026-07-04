package internal

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPToolsDeclareOutputSchemasAndReadOnlyAnnotations(t *testing.T) {
	server := NewMCPServer(&App{config: &Config{}})
	tools := server.GetServer().ListTools()

	wantFields := map[string][]string{
		"get_youtube_metadata": {
			"title",
			"channel",
			"duration_seconds",
			"description",
			"has_captions",
		},
		"get_youtube_transcript": {
			"url",
			"transcript",
			"source",
			"include_timestamps",
		},
		"transcribe_youtube_whisper": {
			"url",
			"transcript",
			"source",
			"include_timestamps",
		},
	}

	for name, fields := range wantFields {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("tool %q is not registered", name)
		}

		if tool.Tool.OutputSchema.Type != "object" {
			t.Errorf("%s output schema type = %q, want object", name, tool.Tool.OutputSchema.Type)
		}

		for _, field := range fields {
			if _, ok := tool.Tool.OutputSchema.Properties[field]; !ok {
				t.Errorf("%s output schema missing field %q", name, field)
			}
		}

		if tool.Tool.Annotations.ReadOnlyHint == nil || !*tool.Tool.Annotations.ReadOnlyHint {
			t.Errorf("%s readOnlyHint is not true", name)
		}

		if tool.Tool.Annotations.DestructiveHint == nil || *tool.Tool.Annotations.DestructiveHint {
			t.Errorf("%s destructiveHint is not false", name)
		}

		if tool.Tool.Annotations.OpenWorldHint == nil || !*tool.Tool.Annotations.OpenWorldHint {
			t.Errorf("%s openWorldHint is not true", name)
		}
	}
}

func TestMCPToolDescriptionsDoNotAdvertisePlaylists(t *testing.T) {
	server := NewMCPServer(&App{config: &Config{}})
	for name, tool := range server.GetServer().ListTools() {
		if strings.Contains(strings.ToLower(tool.Tool.Description), "playlist") {
			t.Fatalf("tool %q description advertises playlist support: %q", name, tool.Tool.Description)
		}
	}
}

func TestMCPServerRejectsInvalidTransport(t *testing.T) {
	server := NewMCPServer(&App{config: &Config{}})
	err := server.Start(context.Background(), "htp", "127.0.0.1", 8765)
	if err == nil {
		t.Fatal("expected invalid transport error")
	}
	if !strings.Contains(err.Error(), "invalid MCP transport") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMCPServerSerializesStdioToolHandlers(t *testing.T) {
	server := &MCPServer{}

	var active atomic.Int32
	var maxActive atomic.Int32

	handler := server.serializeStdioToolCalls(func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		current := active.Add(1)
		for {
			observed := maxActive.Load()
			if current <= observed || maxActive.CompareAndSwap(observed, current) {
				break
			}
		}

		time.Sleep(5 * time.Millisecond)
		active.Add(-1)

		return mcp.NewToolResultText("ok"), nil
	})

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := handler(context.Background(), mcp.CallToolRequest{}); err != nil {
				t.Errorf("handler returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent handlers = %d, want 1", got)
	}
}
