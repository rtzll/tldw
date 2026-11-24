//go:build smoke

package smoke_test

import (
	"bytes"
	"context"
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
	videoURL := os.Getenv("TLDW_SMOKE_VIDEO_URL")
	if videoURL == "" {
		videoURL = readmeVideoURL
	}

	root := repositoryRoot(t)
	binary := filepath.Join(t.TempDir(), "tldw")
	run(t, root, nil, "go", "build", "-o", binary, ".")
	transcribeCLI := func(env []string) string {
		t.Helper()
		result := strings.TrimSpace(run(t, root, env, binary, "transcribe", videoURL, "--timestamps", "--quiet"))
		if !strings.HasPrefix(result, "[") {
			t.Fatalf("expected timestamped CLI transcript, got %.100q", result)
		}
		return result
	}
	transcribeCLI(isolatedXDG(t, "cli"))

	mcpEnv := isolatedXDG(t, "mcp")
	ctx, session := startSmokeMCP(t, binary, mcpEnv)

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
			"url":                videoURL,
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
	// Fresh downloads can have different cue boundaries. Compare the exact
	// rendering of the same captions and prove the CLI can read them offline.
	cached := transcribeCLI(append(mcpEnv, "PATH="+t.TempDir()))
	if mcpTranscript != cached {
		t.Fatalf("MCP and cached CLI transcripts differ: MCP %d bytes, CLI %d bytes", len(mcpTranscript), len(cached))
	}
	t.Logf("CLI and MCP fetched captions independently; the offline CLI matched all %d bytes from MCP", len(cached))
}

// This live check exercises only metadata and free captions, with a cold cache per transport.
func TestSingleVideoXPostsThroughCLIAndMCP(t *testing.T) {
	root := repositoryRoot(t)
	binary := filepath.Join(t.TempDir(), "tldw")
	run(t, root, nil, "go", "build", "-o", binary, ".")
	cliEnv := isolatedXDG(t, "cli")
	ctx, session := startSmokeMCP(t, binary, isolatedXDG(t, "mcp"))
	for _, tc := range []struct {
		name, url string
		captions  bool
	}{
		{"captioned", "https://x.com/AmpCode/status/2123051353505443912", true},
		{"uncaptioned", "https://x.com/creatine_cycle/status/2123846497540489345", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(ctx, binary, "transcribe", tc.url, "--quiet")
			cmd.Env = append(os.Environ(), cliEnv...)
			var cliText, cliError bytes.Buffer
			cmd.Stdout, cmd.Stderr = &cliText, &cliError
			start := time.Now()
			cliErr := cmd.Run()
			cliTime := time.Since(start)
			start = time.Now()
			result, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name: "get_youtube_transcript", Arguments: map[string]any{"url": tc.url},
			})
			mcpTime := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			mcpText := textContent(result)
			if tc.captions {
				if cliErr != nil || result.IsError {
					t.Fatalf("CLI: %v %s; MCP: %.300s", cliErr, cliError.String(), mcpText)
				}
				if mcpText == "" || strings.TrimSpace(cliText.String()) != strings.TrimSpace(mcpText) {
					t.Fatal("expected matching, nonempty CLI and MCP transcripts")
				}
				t.Logf("Fetched %d bytes of captions in %v via CLI and %v via MCP", len(mcpText), cliTime.Round(time.Millisecond), mcpTime.Round(time.Millisecond))
			} else {
				if cliErr == nil || !strings.Contains(cliError.String(), "--allow-paid-transcription") ||
					!result.IsError || !strings.Contains(mcpText, "transcribe_video_paid") {
					t.Fatalf("expected paid opt-in offers. CLI: %v %.300s; MCP: %.300s", cliErr, cliError.String(), mcpText)
				}
				t.Logf("MCP returned %s", mcpText)
			}
		})
	}
}

func startSmokeMCP(t *testing.T, binary string, xdg []string) (context.Context, *mcp.ClientSession) {
	t.Helper()
	port := unusedPort(t)
	server := exec.Command(binary, "mcp", "--transport=http", "--host=127.0.0.1", fmt.Sprintf("--port=%d", port))
	server.Env = append(os.Environ(), xdg...)
	process := &smokeProcess{cmd: server, done: make(chan struct{})}
	server.Stdout, server.Stderr = &process.log, &process.log
	if err := server.Start(); err != nil {
		t.Fatalf("start MCP server: %v", err)
	}
	go func() {
		process.err = server.Wait()
		close(process.done)
	}()
	t.Cleanup(func() { process.stop(t) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "tldw-smoke-test", Version: "1"}, nil)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	session := process.connect(t, ctx, client, endpoint)
	t.Cleanup(func() { _ = session.Close() })
	return ctx, session
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

// Wait owns writes to err and log. Closing done publishes both to readers.
type smokeProcess struct {
	cmd  *exec.Cmd
	log  bytes.Buffer
	done chan struct{}
	err  error
}

func (p *smokeProcess) connect(t *testing.T, ctx context.Context, client *mcp.Client, endpoint string) *mcp.ClientSession {
	t.Helper()
	for {
		session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:             endpoint,
			DisableStandaloneSSE: true,
		}, nil)
		if err == nil {
			return session
		}
		select {
		case <-p.done:
			t.Fatalf("MCP server exited before accepting connections: %v\n%s", p.err, p.log.String())
		case <-ctx.Done():
			t.Fatalf("connect to MCP server: %v (last error: %v)", ctx.Err(), err)
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

func (p *smokeProcess) stop(t *testing.T) {
	t.Helper()
	_ = p.cmd.Process.Signal(os.Interrupt)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
		t.Errorf("MCP server required forced shutdown: %s", p.log.String())
	}
	if t.Failed() {
		t.Logf("MCP server exit: %v\n%s", p.err, p.log.String())
	}
}
