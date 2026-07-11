package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolsDeclareSchemasDescriptionsAndAnnotations(t *testing.T) {
	server := NewMCPServer(newTestMCPApp(t))
	ctx, clientSession := connectTestMCPClient(t, server)

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(res.Tools) != 3 {
		t.Fatalf("tool count = %d, want 3", len(res.Tools))
	}

	tools := make(map[string]*mcp.Tool)
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}

	want := map[string]struct {
		description   string
		inputFields   map[string]string
		requiredInput []string
		outputFields  []string
		readOnly      bool
	}{
		"get_youtube_metadata": {
			description: "Extract video metadata including caption availability. Check 'Has Captions' field to determine which transcript tool to use: if true, use get_youtube_transcript (free); if false, consider transcribe_youtube_whisper (paid).",
			inputFields: map[string]string{
				"url": "YouTube video URL",
			},
			requiredInput: []string{"url"},
			outputFields: []string{
				"title",
				"channel",
				"creators",
				"duration_seconds",
				"description",
				"has_captions",
			},
			readOnly: true,
		},
		"get_youtube_transcript": {
			description: "Get existing YouTube captions/transcript (FREE). Only works if the video has captions - check metadata first. Fails if no captions available.",
			inputFields: map[string]string{
				"url":                "YouTube video URL",
				"include_timestamps": "When true, return transcript lines with timestamps if caption timing data is available.",
			},
			requiredInput: []string{"url"},
			outputFields: []string{
				"url",
				"transcript",
				"source",
				"include_timestamps",
			},
			readOnly: true,
		},
		"transcribe_youtube_whisper": {
			description: "Create transcript using OpenAI Whisper API (PAID). Requires OPENAI_API_KEY environment variable to be set. Use only when videos have no captions and user explicitly agrees to incur costs. Always ask user for confirmation before calling this tool.",
			inputFields: map[string]string{
				"url":                "YouTube video URL",
				"include_timestamps": "Reserved for future use. Timestamped Whisper transcripts are not supported yet.",
			},
			requiredInput: []string{"url"},
			outputFields: []string{
				"url",
				"transcript",
				"source",
				"include_timestamps",
			},
			readOnly: false,
		},
	}

	for name, wantTool := range want {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("tool %q is not registered", name)
		}
		if tool.Description != wantTool.description {
			t.Errorf("%s description = %q, want %q", name, tool.Description, wantTool.description)
		}

		inputSchema := schemaMap(t, tool.InputSchema)
		if got := inputSchema["type"]; got != "object" {
			t.Errorf("%s input schema type = %v, want object", name, got)
		}
		inputProperties := schemaProperties(t, inputSchema)
		for field, wantDescription := range wantTool.inputFields {
			prop := schemaProperty(t, inputProperties, field)
			if got := prop["description"]; got != wantDescription {
				t.Errorf("%s input field %q description = %v, want %q", name, field, got, wantDescription)
			}
		}
		for _, field := range wantTool.requiredInput {
			if !schemaRequired(inputSchema, field) {
				t.Errorf("%s input schema does not require %q", name, field)
			}
		}
		if schemaRequired(inputSchema, "include_timestamps") {
			t.Errorf("%s input schema requires include_timestamps, want optional", name)
		}

		outputSchema := schemaMap(t, tool.OutputSchema)
		if got := outputSchema["type"]; got != "object" {
			t.Errorf("%s output schema type = %v, want object", name, got)
		}
		outputProperties := schemaProperties(t, outputSchema)
		for _, field := range wantTool.outputFields {
			if _, ok := outputProperties[field]; !ok {
				t.Errorf("%s output schema missing field %q", name, field)
			}
		}

		if tool.Annotations == nil {
			t.Fatalf("%s annotations are nil", name)
		}
		if tool.Annotations.ReadOnlyHint != wantTool.readOnly {
			t.Errorf("%s readOnlyHint = %t, want %t", name, tool.Annotations.ReadOnlyHint, wantTool.readOnly)
		}
		if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Errorf("%s destructiveHint is not false", name)
		}
		if tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
			t.Errorf("%s openWorldHint is not true", name)
		}
	}
}

func TestMCPToolDescriptionsDoNotAdvertisePlaylists(t *testing.T) {
	server := NewMCPServer(newTestMCPApp(t))
	ctx, clientSession := connectTestMCPClient(t, server)

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range res.Tools {
		if strings.Contains(strings.ToLower(tool.Description), "playlist") {
			t.Fatalf("tool %q description advertises playlist support: %q", tool.Name, tool.Description)
		}
	}
}

func TestMCPGetMetadataReturnsTextAndStructuredContent(t *testing.T) {
	app := newTestMCPApp(t)
	testYouTube(t, app).executor = &mockCommandRunner{output: []byte(`{
		"title": "Test Video",
		"description": "Test Description",
		"channel": "Test Channel",
		"creators": ["Test Channel", "Guest Creator"],
		"duration": 42,
		"language": "en",
		"categories": ["Education"],
		"tags": ["go", "mcp"],
		"chapters": [{"start_time": 0, "end_time": 10, "title": "Intro"}],
		"subtitles": {"en": [{}]}
	}`)}

	server := NewMCPServer(app)
	ctx, clientSession := connectTestMCPClient(t, server)

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_youtube_metadata",
		Arguments: map[string]any{
			"url": "https://youtu.be/dQw4w9WgXcQ",
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %s", textContent(t, result))
	}

	text := textContent(t, result)
	for _, want := range []string{
		"Title: Test Video",
		"Channel: Test Channel",
		"Creators: Test Channel, Guest Creator",
		"Duration: 42 seconds",
		"Has Captions: true",
		"Chapter (0–10): Intro",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text content missing %q in:\n%s", want, text)
		}
	}

	output := structuredContent[mcpMetadataOutput](t, result)
	if output.Title != "Test Video" {
		t.Errorf("structured title = %q, want Test Video", output.Title)
	}
	if len(output.Creators) != 2 || output.Creators[1] != "Guest Creator" {
		t.Errorf("structured creators = %#v, want guest creator", output.Creators)
	}
	if !output.HasCaptions {
		t.Error("structured has_captions = false, want true")
	}
	if len(output.CaptionLanguages) != 1 || output.CaptionLanguages[0] != "en" {
		t.Errorf("structured caption_languages = %#v, want [en]", output.CaptionLanguages)
	}
	if len(output.Chapters) != 1 || output.Chapters[0].Title != "Intro" {
		t.Errorf("structured chapters = %#v, want Intro chapter", output.Chapters)
	}
}

func TestMCPGetTranscriptReturnsTextAndStructuredContent(t *testing.T) {
	app := newTestMCPApp(t)
	if err := SaveStructuredTranscript(&Transcript{
		VideoID:  "dQw4w9WgXcQ",
		Language: "en",
		Source:   TranscriptSourceCaptions,
		Segments: []TranscriptSegment{
			{Start: 1, End: 3, Text: "Hello world"},
		},
	}, app.config.TranscriptsDir); err != nil {
		t.Fatalf("SaveStructuredTranscript() error = %v", err)
	}

	server := NewMCPServer(app)
	ctx, clientSession := connectTestMCPClient(t, server)

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_youtube_transcript",
		Arguments: map[string]any{
			"url":                "https://youtu.be/dQw4w9WgXcQ",
			"include_timestamps": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %s", textContent(t, result))
	}

	const wantTranscript = "[00:01] Hello world"
	if got := textContent(t, result); got != wantTranscript {
		t.Fatalf("text content = %q, want %q", got, wantTranscript)
	}

	output := structuredContent[mcpTranscriptOutput](t, result)
	if output.URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("structured URL = %q, want normalized URL", output.URL)
	}
	if output.Transcript != wantTranscript {
		t.Errorf("structured transcript = %q, want %q", output.Transcript, wantTranscript)
	}
	if output.Source != string(TranscriptSourceCaptions) {
		t.Errorf("structured source = %q, want captions", output.Source)
	}
	if !output.IncludeTimestamps {
		t.Error("structured include_timestamps = false, want true")
	}
}

func TestMCPWhisperReturnsTextAndStructuredContent(t *testing.T) {
	app := newTestMCPApp(t)
	audioPath := filepath.Join(app.config.CacheDir, "dQw4w9WgXcQ.mp3")
	testYouTube(t, app).executor = &writeAudioCommandRunner{audioPath: audioPath}
	audio := NewAudio(&mockCommandRunner{}, t.TempDir(), false)
	app.Engine = newTestEngine(app.config,
		WithVideoAdapter(app.youtube),
		WithAI(NewAI(&fixedOpenAIClient{text: "whisper transcript"}, audio, "whisper-1", WhisperLimit, 0, false, true)),
	)

	server := NewMCPServer(app)
	ctx, clientSession := connectTestMCPClient(t, server)

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "transcribe_youtube_whisper",
		Arguments: map[string]any{
			"url": "https://youtu.be/dQw4w9WgXcQ",
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %s", textContent(t, result))
	}
	if got := textContent(t, result); got != "whisper transcript" {
		t.Fatalf("text content = %q, want whisper transcript", got)
	}

	output := structuredContent[mcpTranscriptOutput](t, result)
	if output.URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("structured URL = %q, want normalized URL", output.URL)
	}
	if output.Transcript != "whisper transcript" {
		t.Errorf("structured transcript = %q, want whisper transcript", output.Transcript)
	}
	if output.Source != string(TranscriptSourceWhisper) {
		t.Errorf("structured source = %q, want whisper", output.Source)
	}
	if output.IncludeTimestamps {
		t.Error("structured include_timestamps = true, want false")
	}
}

func TestMCPWhisperRejectsTimestampsBeforeDownload(t *testing.T) {
	app := newTestMCPApp(t)
	runner := &writeAudioCommandRunner{audioPath: filepath.Join(app.config.CacheDir, "dQw4w9WgXcQ.mp3")}
	testYouTube(t, app).executor = runner

	server := NewMCPServer(app)
	ctx, clientSession := connectTestMCPClient(t, server)

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "transcribe_youtube_whisper",
		Arguments: map[string]any{
			"url":                "https://youtu.be/dQw4w9WgXcQ",
			"include_timestamps": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatal("CallTool() succeeded, want timestamp support error")
	}
	if got := textContent(t, result); !strings.Contains(got, "timestamped Whisper transcripts are not supported yet") {
		t.Fatalf("tool error text = %q, want timestamp unsupported message", got)
	}
	if runner.calls.Load() != 0 {
		t.Fatalf("download command was called %d times, want 0", runner.calls.Load())
	}
}

func TestMCPHTTPTransportServesTools(t *testing.T) {
	server := NewMCPServer(newTestMCPApp(t))
	port := unusedTCPPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx, "http", "127.0.0.1", port)
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("HTTP server returned error: %v", err)
		}
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	var (
		clientSession *mcp.ClientSession
		err           error
	)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		clientSession, err = client.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:             endpoint,
			DisableStandaloneSSE: true,
		}, nil)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("client Connect(%s) error = %v", endpoint, err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(res.Tools) != 3 {
		t.Fatalf("tool count over HTTP = %d, want 3", len(res.Tools))
	}
}

func TestMCPServerRejectsInvalidTransport(t *testing.T) {
	server := NewMCPServer(newTestMCPApp(t))
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

	handler := server.serializeStdioToolCalls(func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		current := active.Add(1)
		for {
			observed := maxActive.Load()
			if current <= observed || maxActive.CompareAndSwap(observed, current) {
				break
			}
		}

		time.Sleep(5 * time.Millisecond)
		active.Add(-1)

		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	})

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := handler(context.Background(), mcpMethodCallTool, nil); err != nil {
				t.Errorf("handler returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent handlers = %d, want 1", got)
	}
}

type mcpTestHarness struct {
	*Engine
	config  *Config
	youtube *YouTube
}

func newTestMCPApp(t *testing.T) *mcpTestHarness {
	t.Helper()

	baseDir := t.TempDir()
	config := &Config{
		CacheDir:       filepath.Join(baseDir, "cache"),
		ConfigDir:      filepath.Join(baseDir, "config"),
		TempDir:        filepath.Join(baseDir, "tmp"),
		TranscriptsDir: filepath.Join(baseDir, "transcripts"),
		Quiet:          true,
	}
	for _, dir := range []string{config.CacheDir, config.ConfigDir, config.TempDir, config.TranscriptsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}

	youtube := NewYouTubeWithCache(config.TranscriptsDir, config.CacheDir, false, true)
	return &mcpTestHarness{
		Engine:  newTestEngine(config, WithVideoAdapter(youtube)),
		config:  config,
		youtube: youtube,
	}
}

func testYouTube(t *testing.T, app *mcpTestHarness) *YouTube {
	t.Helper()
	return app.youtube
}

func unusedTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding unused TCP port: %v", err)
	}
	defer func() { _ = listener.Close() }()

	return listener.Addr().(*net.TCPAddr).Port
}

func connectTestMCPClient(t *testing.T, server *MCPServer) (context.Context, *mcp.ClientSession) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.GetServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return ctx, clientSession
}

func schemaMap(t *testing.T, schema any) map[string]any {
	t.Helper()

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshaling schema: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshaling schema %s: %v", data, err)
	}
	return out
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %T, want object", schema["properties"])
	}
	return properties
}

func schemaProperty(t *testing.T, properties map[string]any, field string) map[string]any {
	t.Helper()

	prop, ok := properties[field].(map[string]any)
	if !ok {
		t.Fatalf("schema property %q = %T, want object", field, properties[field])
	}
	return prop
}

func schemaRequired(schema map[string]any, field string) bool {
	required, _ := schema["required"].([]any)
	for _, item := range required {
		if item == field {
			return true
		}
	}
	return false
}

func textContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	if len(result.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want *mcp.TextContent", result.Content[0])
	}
	return text.Text
}

func structuredContent[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()

	if result.StructuredContent == nil {
		t.Fatal("structuredContent is nil")
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling structuredContent: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshaling structuredContent %s: %v", data, err)
	}
	return out
}

type writeAudioCommandRunner struct {
	audioPath string
	calls     atomic.Int32
}

func (r *writeAudioCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.calls.Add(1)
	if err := os.MkdirAll(filepath.Dir(r.audioPath), 0755); err != nil {
		return nil, err
	}
	return nil, os.WriteFile(r.audioPath, []byte("audio"), 0644)
}

func (r *writeAudioCommandRunner) RunStreaming(ctx context.Context, name string, args []string, onLine func(string)) error {
	_, err := r.Run(ctx, name, args...)
	return err
}

type fixedOpenAIClient struct {
	text string
}

func (c *fixedOpenAIClient) CreateTranscription(ctx context.Context, file *os.File) (string, error) {
	return c.text, nil
}

func (c *fixedOpenAIClient) CreateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
	return "", nil
}
