package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rtzll/tldw/internal/tldw"
)

// Use explicit wire requests so SDK client fallback cannot hide a discovery
// response that omits the protocol version the caller needs.
func TestMCPHTTPProtocolCompatibility(t *testing.T) {
	for _, path := range []string{"/", "/mcp"} {
		t.Run(path, func(t *testing.T) {
			for _, version := range []string{"2026-07-28", "2025-11-25"} {
				t.Run(version, func(t *testing.T) {
					app := &applicationStub{metadata: &tldw.VideoMetadata{
						Title: "HTTP compatibility", Channel: "Test channel", Duration: 42,
					}}
					endpoint := startCompatibilityHTTPServer(t, app) + path
					if version == "2026-07-28" {
						var discovery mcp.DiscoverResult
						requestCompatibilityMCP(t, endpoint, version, "server/discover", nil, &discovery)
						if !slices.Contains(discovery.SupportedVersions, version) {
							t.Fatalf("discovery supportedVersions = %v, want %s", discovery.SupportedVersions, version)
						}
					} else {
						var initialized mcp.InitializeResult
						requestCompatibilityMCP(t, endpoint, version, "initialize", map[string]any{
							"protocolVersion": version,
							"capabilities":    map[string]any{},
							"clientInfo":      map[string]any{"name": "compatibility-test", "version": "1"},
						}, &initialized)
						if initialized.ProtocolVersion != version {
							t.Fatalf("initialize protocolVersion = %q, want %s", initialized.ProtocolVersion, version)
						}
						requestCompatibilityMCP(t, endpoint, version, "notifications/initialized", nil, nil)
					}

					// No session ID is sent, for either protocol generation.
					var listed mcp.ListToolsResult
					requestCompatibilityMCP(t, endpoint, version, "tools/list", nil, &listed)
					if len(listed.Tools) != 3 {
						t.Fatalf("tools/list returned %d tools, want 3", len(listed.Tools))
					}
					var result mcp.CallToolResult
					requestCompatibilityMCP(t, endpoint, version, "tools/call", map[string]any{
						"name":      "get_youtube_metadata",
						"arguments": map[string]any{"url": "https://youtu.be/dQw4w9WgXcQ"},
					}, &result)
					if result.IsError {
						t.Fatalf("tools/call returned tool error: %s", textContent(t, &result))
					}
					if output := structuredContent[mcpMetadataOutput](t, &result); output.Title != app.metadata.Title {
						t.Fatalf("metadata title = %q, want %q", output.Title, app.metadata.Title)
					}
					if app.metadataCalls != 1 {
						t.Fatalf("metadata calls = %d, want 1", app.metadataCalls)
					}
				})
			}
		})
	}
}

func startCompatibilityHTTPServer(t *testing.T, app MCPApplication) string {
	t.Helper()
	port := unusedTCPPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewMCPServer(app).Start(ctx, "http", "127.0.0.1", port)
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("HTTP server returned error: %v", err)
		}
	})
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return "http://" + address
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("HTTP server did not become reachable")
	return ""
}

func requestCompatibilityMCP(t *testing.T, endpoint, version, method string, params map[string]any, result any) {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	if version == "2026-07-28" {
		params["_meta"] = map[string]any{
			mcp.MetaKeyProtocolVersion:    version,
			mcp.MetaKeyClientInfo:         map[string]any{"name": "compatibility-test", "version": "1"},
			mcp.MetaKeyClientCapabilities: map[string]any{},
		}
	}
	notification := strings.HasPrefix(method, "notifications/")
	message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if !notification {
		message["id"] = method
	}
	body, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", version)
	req.Header.Set("MCP-Method", method)
	if name, ok := params["name"].(string); ok {
		req.Header.Set("MCP-Name", name)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s response: %v", method, err)
	}
	if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("%s returned session ID %q, want stateless HTTP", method, got)
	}
	if notification {
		if resp.StatusCode != http.StatusAccepted || len(body) != 0 {
			t.Fatalf("%s status = %d, body = %s; want empty 202", method, resp.StatusCode, body)
		}
		return
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status = %d, body = %s; want 200", method, resp.StatusCode, body)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		var data []string
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(line, "data:"))
			}
		}
		body = []byte(strings.Join(data, "\n"))
	} else if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("%s unexpected content type %q", method, resp.Header.Get("Content-Type"))
	}
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decoding %s response %s: %v", method, body, err)
	}
	if response.JSONRPC != "2.0" || response.ID != method || len(response.Error) != 0 {
		t.Fatalf("invalid %s JSON-RPC response: %s", method, body)
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		t.Fatalf("decoding %s result: %v", method, err)
	}
}

type cancellationApp struct {
	started  chan struct{}
	canceled chan struct{}
}

func (a *cancellationApp) MetadataFor(ctx context.Context, _ tldw.YouTubeRef) (*tldw.VideoMetadata, error) {
	close(a.started)
	<-ctx.Done()
	close(a.canceled)
	return nil, ctx.Err()
}

func (a *cancellationApp) Transcript(context.Context, tldw.YouTubeRef, tldw.TranscriptRequest) (*tldw.Transcript, error) {
	return nil, errors.New("unexpected transcript request")
}

func TestHTTPDisconnectCancelsTool(t *testing.T) {
	app := &cancellationApp{started: make(chan struct{}), canceled: make(chan struct{})}
	endpoint := startCompatibilityHTTPServer(t, app) + "/mcp"
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_youtube_metadata","arguments":{"url":"https://youtu.be/dQw4w9WgXcQ"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("MCP-Method", "tools/call")
	req.Header.Set("MCP-Name", "get_youtube_metadata")
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-app.started:
	case <-time.After(3 * time.Second):
		t.Fatal("tool did not start")
	}
	cancel()
	select {
	case <-app.canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("tool context not canceled after disconnect")
	}
	<-done
}
