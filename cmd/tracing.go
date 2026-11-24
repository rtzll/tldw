package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rtzll/tldw/internal/telemetry"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// The command owns its sinks. Shutdown runs before closing the local file,
// including on cancellation and RunE errors.
func withTracing(cmd *cobra.Command, path string, run func(trace.Tracer, io.Writer) error) error {
	var local io.Writer
	if path != "" {
		f, err := openTraceLog(path)
		if err != nil {
			cmd.PrintErrf("Opening trace log: %v\n", err)
		} else {
			defer f.Close()
			local = f
		}
	}
	provider, err := telemetry.NewProvider(cmd.Context(), version, local)
	if err != nil {
		cmd.PrintErrln("Could not configure OTLP export")
	}
	defer func() {
		if err := telemetry.Shutdown(provider); err != nil {
			cmd.PrintErrln("Trace export did not finish before shutdown")
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

func tracedCLI(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		path := os.Getenv("TLDW_TRACE_LOG")
		if path == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
			return run(cmd, args)
		}
		return withTracing(cmd, path, func(tracer trace.Tracer, _ io.Writer) error {
			parent := cmd.Context()
			ctx, span := tracer.Start(parent, cmd.CommandPath())
			cmd.SetContext(ctx)
			defer cmd.SetContext(parent)
			defer span.End()
			if err := run(cmd, args); err != nil {
				_, failure := classify(err)
				span.SetStatus(codes.Error, failure.Reason)
				span.SetAttributes(attribute.String("error.type", failure.Reason))
				return fmt.Errorf("%w [trace_id=%s]", err, span.SpanContext().TraceID())
			}
			return nil
		})
	}
}
