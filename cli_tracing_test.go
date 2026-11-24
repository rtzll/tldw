package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestCLIArgumentErrorCarriesLocalTraceID(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "trace.jsonl")
	cmd := cliCommand(t, dir, "metadata")
	cmd.Env = append(cmd.Env, "TLDW_TRACE_LOG="+logPath)
	out, diag, code := runCLI(t, cmd)
	var failure struct{ Reason, Message string }
	if code != 2 || out != "" || json.Unmarshal([]byte(diag), &failure) != nil || failure.Reason != "invalid_arguments" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, diag)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var span struct {
		ID      string `json:"trace_id"`
		Event   string `json:"event"`
		Outcome string `json:"outcome"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		if err := decoder.Decode(&span); err != nil {
			t.Fatal(err)
		}
	}
	if len(span.ID) != 32 || span.Event != "span.end" || span.Outcome != "error" || !strings.Contains(failure.Message, "[trace_id="+span.ID+"]") {
		t.Fatalf("stderr=%s trace=%s", diag, data)
	}
}

// Exercise the shipped CLI entry point, short-lived batch flush, and the wire
// format accepted by an OTLP/HTTP collector, using a real cached transcript.
func TestCLITraceExportMatchesLocalLog(t *testing.T) {
	dir := t.TempDir()
	const id = "dQw4w9WgXcQ"
	const text = "transcript body must stay out of telemetry"
	cached, err := tldw.NewCaptionTranscript(id, tldw.CaptionTrack{Language: "en", Direct: true}, text, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewFile(filepath.Join(dir, "data", "tldw", "transcripts")).SaveTranscript(cached); err != nil {
		t.Fatal(err)
	}
	received := make(chan *collector.ExportTraceServiceRequest, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/traces" {
			t.Errorf("unexpected OTLP request %s %s", r.Method, r.URL.Path)
		}
		buf, err := io.ReadAll(r.Body)
		var batch collector.ExportTraceServiceRequest
		if err != nil || bytes.Contains(buf, []byte(text)) || proto.Unmarshal(buf, &batch) != nil {
			t.Error("invalid or unsanitized OTLP batch")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- &batch
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	logPath := filepath.Join(dir, "traces.jsonl")
	command := cliCommand(t, dir, "transcribe", id)
	command.Env = append(command.Env, "OTEL_EXPORTER_OTLP_ENDPOINT="+server.URL, "TLDW_TRACE_LOG="+logPath)
	out, diag, code := runCLI(t, command)
	if code != 0 || out != text+"\n" || diag != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, diag)
	}
	close(received)
	rootID := ""
	names := map[string]bool{}
	for batch := range received {
		for _, resource := range batch.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				for _, s := range scope.Spans {
					names[s.Name] = true
					if s.Name == "tldw transcribe" {
						rootID = hex.EncodeToString(s.TraceId)
					}
				}
			}
		}
	}
	if rootID == "" || !names["transcript.acquire"] || !names["lock.filesystem.wait"] || names["yt-dlp.captions"] {
		t.Fatalf("unexpected exported spans: %v", names)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(text)) {
		t.Fatal("transcript in local trace")
	}
	hit := false
	for line := range strings.Lines(string(data)) {
		var entry struct {
			ID    string         `json:"trace_id"`
			Attrs map[string]any `json:"attributes"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.ID != rootID {
			t.Fatalf("mismatched local record: %s %v", line, err)
		}
		if entry.Attrs["tldw.cache.result"] == "hit" {
			hit = true
		}
	}
	if !hit {
		t.Fatal("missing transcript cache hit")
	}
}
