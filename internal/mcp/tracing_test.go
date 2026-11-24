package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rtzll/tldw/internal/telemetry"
)

func TestToolErrorCarriesIncomingTraceToTheChatAndLocalLog(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var local bytes.Buffer
	p, err := telemetry.NewProvider(t.Context(), "test", &local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = telemetry.Shutdown(p) })
	srv := NewMCPServer(&applicationStub{transcriptErr: context.DeadlineExceeded}, nil, p.Tracer("test"))
	ctx, client := connectTestMCPClient(t, srv)
	const id = "1234567890abcdef1234567890abcdef"
	const parent = "abcdef1234567890"
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "get_youtube_transcript",
		Meta:      mcp.Meta{"traceparent": "00-" + id + "-" + parent + "-01", "baggage": "should-never-appear"},
		Arguments: map[string]any{"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ&secret=should-never-appear"}})
	if err != nil || !result.IsError || !strings.Contains(textContent(t, result), "trace_id="+id) {
		t.Fatalf("unexpected MCP error result: %+v, %v", result, err)
	}
	var s struct {
		Trace   string `json:"trace_id"`
		Parent  string `json:"parent_span_id"`
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(local.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Trace != id || s.Parent != parent || s.Outcome != "error" || strings.Contains(local.String(), "should-never-appear") {
		t.Fatalf("unexpected trace %s", local.String())
	}
}
