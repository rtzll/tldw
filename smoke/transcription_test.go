//go:build smoke

package smoke_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const readmeVideoURL = "https://youtu.be/tAP1eZYEuKA"

func TestTranscriptionThroughCLIAndMCP(t *testing.T) {
	requireExecutable(t, "yt-dlp")

	root := repositoryRoot(t)
	binary := filepath.Join(t.TempDir(), "tldw")
	run(t, root, nil, "go", "build", "-o", binary, ".")

	cliTranscript := strings.TrimSpace(run(t, root, isolatedXDG(t, "cli"), binary,
		"transcribe", readmeVideoURL, "--timestamps", "--quiet"))
	if cliTranscript == "" {
		t.Fatal("CLI returned an empty transcript")
	}
	if !strings.HasPrefix(cliTranscript, "[") {
		t.Fatalf("CLI transcript does not appear to contain timestamps: %.100q", cliTranscript)
	}

	port := unusedPort(t)
	server := exec.Command(binary, "mcp", "--transport=http", "--host=127.0.0.1", fmt.Sprintf("--port=%d", port))
	server.Env = append(os.Environ(), isolatedXDG(t, "mcp")...)
	var serverLog bytes.Buffer
	server.Stdout = &serverLog
	server.Stderr = &serverLog
	if err := server.Start(); err != nil {
		t.Fatalf("start MCP server: %v", err)
	}
	serverResult := &processResult{done: make(chan struct{})}
	go func() {
		serverResult.err = server.Wait()
		close(serverResult.done)
	}()
	t.Cleanup(func() { stopProcess(t, server, serverResult, &serverLog) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "tldw-smoke-test", Version: "1"}, nil)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	session := connectMCP(t, ctx, client, endpoint, serverResult, &serverLog)
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list MCP tools: %v", err)
	}
	if !hasTool(tools.Tools, "get_youtube_transcript") {
		t.Fatal("MCP server did not advertise get_youtube_transcript")
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_youtube_transcript",
		Arguments: map[string]any{
			"url":                readmeVideoURL,
			"include_timestamps": true,
		},
	})
	if err != nil {
		t.Fatalf("call get_youtube_transcript: %v", err)
	}
	if result.IsError {
		t.Fatalf("get_youtube_transcript returned an error: %s", textContent(result))
	}

	mcpTranscript := strings.TrimSpace(textContent(result))
	if mcpTranscript != cliTranscript {
		t.Fatalf("CLI and MCP transcripts differ\nCLI: %d bytes\nMCP: %d bytes", len(cliTranscript), len(mcpTranscript))
	}
	t.Logf("CLI and MCP returned the same timestamped transcript (%d bytes)", len(cliTranscript))
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate smoke test source")
	}
	return filepath.Dir(filepath.Dir(file))
}

func requireExecutable(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("%s must be installed to run this smoke test: %v", name, err)
	}
}

func isolatedXDG(t *testing.T, name string) []string {
	t.Helper()
	base := filepath.Join(t.TempDir(), name)
	return []string{
		"XDG_CONFIG_HOME=" + filepath.Join(base, "config"),
		"XDG_DATA_HOME=" + filepath.Join(base, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(base, "cache"),
	}
}

func run(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}

func unusedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

type processResult struct {
	done chan struct{}
	err  error
}

func connectMCP(t *testing.T, ctx context.Context, client *mcp.Client, endpoint string, serverResult *processResult, serverLog *bytes.Buffer) *mcp.ClientSession {
	t.Helper()
	var lastErr error
	for {
		session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:             endpoint,
			DisableStandaloneSSE: true,
		}, nil)
		if err == nil {
			return session
		}
		lastErr = err
		select {
		case <-serverResult.done:
			t.Fatalf("MCP server exited before accepting connections: %v\n%s", serverResult.err, serverLog.String())
		case <-ctx.Done():
			t.Fatalf("connect to MCP server: %v (last error: %v)\n%s", ctx.Err(), lastErr, serverLog.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func hasTool(tools []*mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func textContent(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if item, ok := content.(*mcp.TextContent); ok {
			text.WriteString(item.Text)
		}
	}
	return text.String()
}

func stopProcess(t *testing.T, command *exec.Cmd, result *processResult, log *bytes.Buffer) {
	t.Helper()
	if command.Process == nil {
		return
	}
	_ = command.Process.Signal(os.Interrupt)
	select {
	case <-result.done:
		if result.err != nil && !errors.Is(result.err, os.ErrProcessDone) {
			t.Logf("MCP server stopped with %v\n%s", result.err, log.String())
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		<-result.done
		t.Log("MCP server did not stop gracefully; killed it")
	}
}
