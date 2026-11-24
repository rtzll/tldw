package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rtzll/tldw/internal/telemetry"
	"github.com/rtzll/tldw/internal/tldw"
)

func TestDownloadTraceKeepsStatusAndExcludesCommandPayload(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var local bytes.Buffer
	p, err := telemetry.NewProvider(t.Context(), "test", &local)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = telemetry.Shutdown(p) }()
	ctx, parent := p.Tracer("test").Start(t.Context(), "operation")
	defer parent.End()
	dir := t.TempDir()
	client := New(dir, dir, false, true)
	client.executor = commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("private transcript body"), errors.New("ERROR: Unable to download video subtitles: HTTP Error 403: Forbidden https://example.invalid/?token=do-not-export")
	})
	ref, _ := tldw.ParseVideoRef("dQw4w9WgXcQ")
	_, err = client.downloadCaptions(ctx, ref, directTrack{Language: "en-orig", Automatic: true})
	if !errors.Is(err, tldw.ErrDownloadFailed) {
		t.Fatal(err)
	}
	var record struct {
		Attrs   map[string]any `json:"attributes"`
		Outcome string         `json:"outcome"`
	}
	if err := json.Unmarshal(local.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Outcome != "error" || record.Attrs["http.response.status_code"] != 403.0 || record.Attrs["error.type"] != "subtitle_download_failed" || record.Attrs["tldw.caption.language"] != "en-orig" {
		t.Fatalf("%s", local.String())
	}
	for _, secret := range []string{"private transcript", "example.invalid", "do-not-export", dir} {
		if strings.Contains(local.String(), secret) {
			t.Fatal("trace recorded private input")
		}
	}
}
