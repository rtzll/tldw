package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rtzll/tldw/internal/telemetry"
	"github.com/rtzll/tldw/internal/tldw"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type overlappingCalls struct {
	applicationStub
	arrivals atomic.Int32
	release  chan struct{}
}

func (a *overlappingCalls) Transcript(ctx context.Context, ref tldw.ContentRef, _ tldw.TranscriptRequest) (*tldw.Transcript, error) {
	if a.arrivals.Add(1) == 3 {
		close(a.release)
	}
	select {
	case <-a.release:
		if ref.CacheKey() == "dQw4w9WgXcQ" {
			return tldw.NewCaptionTranscript(ref.CacheKey(), tldw.CaptionTrack{Language: "en", Direct: true}, "caption", nil)
		}
		return nil, tldw.ErrCaptionsUnavailable
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestParallelHTTPToolCallsKeepIndependentTraces(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	app := &overlappingCalls{release: make(chan struct{})}
	endpoint := startCompatibilityHTTPServer(t, NewMCPServer(app, "test-build", nil, p.Tracer("test")))
	videoIDs := []string{"dQw4w9WgXcQ", "H15B1j0SAmI", "tAP1eZYEuKA"}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	results := make([]*mcp.CallToolResult, len(videoIDs))
	errs := make([]error, len(videoIDs))
	var group sync.WaitGroup
	for i, id := range videoIDs {
		group.Go(func() {
			results[i], errs[i] = session.CallTool(ctx, &mcp.CallToolParams{
				Name: "get_youtube_transcript", Arguments: map[string]any{"url": id},
			})
		})
	}
	group.Wait()
	toolIDs := map[string]string{}
	roots := map[string]bool{}
	for _, s := range recorder.Ended() {
		id := s.SpanContext().TraceID().String()
		switch s.Name() {
		case "POST /mcp":
			roots[id] = true
		case "tools/call get_youtube_transcript":
			for _, kv := range s.Attributes() {
				if kv.Key == "tldw.video.id" {
					toolIDs[kv.Value.AsString()] = id
				}
			}
		}
	}
	seen := map[string]bool{}
	for i, video := range videoIDs {
		id := toolIDs[video]
		if errs[i] != nil || !roots[id] || seen[id] || results[i].IsError != (i != 0) {
			t.Fatalf("missing or incorrect result for %s: trace=%s err=%v", video, id, errs[i])
		}
		seen[id] = true
		if i != 0 && !strings.Contains(textContent(t, results[i]), "trace_id="+id) {
			t.Fatalf("error for %s did not carry its own trace %s", video, id)
		}
	}
}

func TestHTTPAndToolTraceParents(t *testing.T) {
	const headerTrace = "11111111111111111111111111111111"
	const metaTrace = "22222222222222222222222222222222"
	const remoteParent = "abcdef1234567890"
	for _, tc := range []struct{ name, meta, want string }{
		{"header", "", headerTrace},
		{"invalid meta", "invalid", headerTrace},
		{"separate MCP parent", "00-" + metaTrace + "-" + remoteParent + "-01", metaTrace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			endpoint := startCompatibilityHTTPServer(t, NewMCPServer(&applicationStub{transcriptErr: context.DeadlineExceeded}, "test-build", nil, p.Tracer("test")))
			params := map[string]any{"name": "get_youtube_transcript", "arguments": map[string]any{"url": "dQw4w9WgXcQ"}}
			if tc.meta != "" {
				params["_meta"] = map[string]any{"traceparent": tc.meta}
			}
			headers := http.Header{}
			headers.Set("traceparent", "00-"+headerTrace+"-"+remoteParent+"-01")
			var result mcp.CallToolResult
			requestCompatibilityMCP(t, endpoint+"/mcp", "2026-07-28", "tools/call", params, &result, headers)
			if !result.IsError || !strings.Contains(textContent(t, &result), "trace_id="+tc.want) {
				t.Fatalf("unexpected tool result: %+v", result)
			}
			var ingress, tool sdktrace.ReadOnlySpan
			for _, s := range recorder.Ended() {
				switch s.Name() {
				case "POST /mcp":
					ingress = s
				case "tools/call get_youtube_transcript":
					tool = s
				}
			}
			if ingress == nil || tool == nil || ingress.Parent().SpanID().String() != remoteParent ||
				ingress.SpanContext().TraceID().String() != headerTrace || tool.SpanContext().TraceID().String() != tc.want {
				t.Fatalf("unexpected spans: %v", recorder.Ended())
			}
			if tc.want == headerTrace {
				if tool.Parent().SpanID() != ingress.SpanContext().SpanID() {
					t.Fatal("HTTP arrival is not the tool span's parent")
				}
			} else if tool.Parent().SpanID().String() != remoteParent || len(tool.Links()) != 1 ||
				!tool.Links()[0].SpanContext.Equal(ingress.SpanContext()) || len(ingress.Links()) != 1 ||
				!ingress.Links()[0].SpanContext.Equal(tool.SpanContext()) {
				t.Fatal("separate HTTP and MCP traces are not linked in both directions")
			}
		})
	}
}

func TestTraceHTTPRejectionExcludesRawPath(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/private-path?token=secret", nil)
	traceHTTP(p.Tracer("test"), http.NotFoundHandler()).ServeHTTP(w, r)
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "GET unmatched" || spans[0].Status().Code != codes.Error ||
		w.Header().Get("X-Tldw-Trace-Id") != spans[0].SpanContext().TraceID().String() {
		t.Fatalf("unexpected HTTP trace: %v", spans)
	}
	if !slices.ContainsFunc(spans[0].Attributes(), func(kv attribute.KeyValue) bool {
		return kv.Key == "http.response.status_code" && kv.Value.AsInt64() == http.StatusNotFound
	}) {
		t.Fatal("missing HTTP status")
	}
	for _, kv := range spans[0].Attributes() {
		if strings.Contains(kv.Value.String(), "private-path") || strings.Contains(kv.Value.String(), "secret") {
			t.Fatalf("raw URL in trace: %v", kv)
		}
	}
}

func TestToolErrorCarriesIncomingTraceToTheChatAndLocalLog(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var local bytes.Buffer
	p, err := telemetry.NewProvider(t.Context(), "test", &local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = telemetry.Shutdown(p) })
	srv := NewMCPServer(&applicationStub{transcriptErr: context.DeadlineExceeded}, "test-build", nil, p.Tracer("test"))
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
		Name    string `json:"msg"`
		Event   string `json:"event"`
		Trace   string `json:"trace_id"`
		Parent  string `json:"parent_span_id"`
		Outcome string `json:"outcome"`
	}
	records := json.NewDecoder(bytes.NewReader(local.Bytes()))
	var toolEnded, waited bool
	for records.More() {
		if err := records.Decode(&s); err != nil || s.Trace != id {
			t.Fatalf("record=%+v err=%v", s, err)
		}
		if s.Event == "span.end" {
			switch s.Name {
			case "tools/call get_youtube_transcript":
				toolEnded = s.Parent == parent && s.Outcome == "error"
			case "mcp.stdio.wait":
				waited = s.Outcome == "ok"
			}
		}
	}
	if !toolEnded || !waited || strings.Contains(local.String(), "should-never-appear") {
		t.Fatalf("unexpected trace %s", local.String())
	}
}
