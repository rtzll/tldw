package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/rtzll/tldw/internal/telemetry"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// The command owns its sinks. Shutdown runs before closing the local file,
// including on cancellation and RunE errors.
func withTracing(ctx context.Context, diagnostic io.Writer, path string, run func(trace.Tracer, io.Writer) error) error {
	var local io.Writer
	if path != "" {
		f, err := openTraceLog(path)
		if err != nil {
			_, _ = fmt.Fprintf(diagnostic, "Opening trace log: %v\n", err)
		} else {
			defer func() {
				if err := f.Close(); err != nil {
					_, _ = fmt.Fprintln(diagnostic, "Could not close trace log:", err)
				}
			}()
			local = &traceLogWriter{file: f, diagnostic: diagnostic}
		}
	}
	provider, err := telemetry.NewProvider(ctx, version, local)
	if err != nil {
		_, _ = fmt.Fprintln(diagnostic, "Could not configure OTLP export")
	}
	defer func() {
		if err := telemetry.Shutdown(provider); err != nil {
			_, _ = fmt.Fprintln(diagnostic, "Trace export did not finish before shutdown")
		}
	}()
	if local == nil {
		local = io.Discard
	}
	return run(provider.Tracer(telemetry.Scope), local)
}

func openTraceLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}

// slog does not surface write failures to callers. Report a broken local sink
// once on stderr, allowing the request and any OTLP export to continue.
type traceLogWriter struct {
	file       io.Writer
	diagnostic io.Writer
	warn       sync.Once
}

func (w *traceLogWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	if err != nil {
		w.warn.Do(func() { _, _ = fmt.Fprintln(w.diagnostic, "Could not write local trace log:", err) })
	}
	return n, err
}

func executeCLI(ctx context.Context) (*cobra.Command, error) {
	run := func(ctx context.Context) (*cobra.Command, error) {
		cmd, err := rootCmd.ExecuteContextC(ctx)
		if cause := context.Cause(ctx); cause != nil {
			err = cause
		}
		return cmd, err
	}
	path := os.Getenv("TLDW_TRACE_LOG")
	if path == "" && !telemetry.ExportConfigured() {
		return run(ctx)
	}

	// A long-lived server owns its provider and starts a separate trace per call.
	selected, _, _ := rootCmd.Find(os.Args[1:])
	if selected == mcpCmd {
		return run(ctx)
	}
	if selected == nil {
		selected = rootCmd
	}
	var executed *cobra.Command
	err := withTracing(ctx, rootCmd.ErrOrStderr(), path, func(tracer trace.Tracer, _ io.Writer) error {
		runCtx, span := tracer.Start(ctx, selected.CommandPath())
		defer telemetry.EndSpan(span)
		var err error
		executed, err = run(runCtx)
		if executed != nil {
			span.SetName(executed.CommandPath())
		}
		if err != nil {
			_, failure := classify(err)
			span.SetStatus(codes.Error, failure.Reason)
			span.SetAttributes(attribute.String("error.type", failure.Reason))
			return fmt.Errorf("%w [trace_id=%s]", err, span.SpanContext().TraceID())
		}
		return nil
	})
	return executed, err
}
