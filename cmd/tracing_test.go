package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestLocalTraceWriteFailureReportsOnce(t *testing.T) {
	file, err := openTraceLog(t.TempDir() + "/traces.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	sink := traceLogWriter{file: file, diagnostic: &diagnostic}
	for range 2 {
		if _, err := sink.Write([]byte("private record")); err == nil {
			t.Fatal("expected write error")
		}
	}
	if strings.Count(diagnostic.String(), "Could not write local trace log") != 1 || strings.Contains(diagnostic.String(), "private record") {
		t.Fatalf("unexpected diagnostic: %q", diagnostic.String())
	}
}
