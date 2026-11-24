package tldw

import (
	"context"
	"errors"
	"io/fs"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// EndOperation records a stable failure category. Wrapped adapter errors can
// contain signed URLs or entire provider responses and must not be exported.
func EndOperation(span trace.Span, err error) {
	if err != nil {
		kind := "operation_failed"
		switch {
		case errors.Is(err, context.Canceled):
			kind = "canceled"
		case errors.Is(err, context.DeadlineExceeded):
			kind = "deadline_exceeded"
		case errors.Is(err, ErrRateLimited):
			kind = "rate_limited"
		case errors.Is(err, ErrTranscriptTimestampsUnavailable):
			kind = "timestamps_unavailable"
		case errors.Is(err, ErrCaptionsUnavailable):
			kind = "captions_unavailable"
		case errors.Is(err, ErrDownloadFailed):
			kind = "download_failed"
		case errors.Is(err, fs.ErrPermission):
			kind = "permission_denied"
		case errors.Is(err, fs.ErrNotExist):
			kind = "file_not_found"
		}
		span.SetStatus(codes.Error, kind)
		span.SetAttributes(attribute.String("error.type", kind))
	}
	span.End()
}
