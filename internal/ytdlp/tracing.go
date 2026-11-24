package ytdlp

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/rtzll/tldw/internal/telemetry"
	"github.com/rtzll/tldw/internal/tldw"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var httpStatus = regexp.MustCompile(`(?i)\bhttp error ([1-5][0-9]{2})\b`)

// run traces one actual subprocess, including failed attempts later recovered
// by the caller. Keep raw arguments and process output out of both exporters.
func (yt *Client) run(ctx context.Context, stage string, ref tldw.ContentRef, args []string, attrs ...attribute.KeyValue) ([]byte, error) {
	attrs = append(attrs, attribute.String("tldw.video.id", ref.CacheKey()))
	ctx, span := telemetry.Start(ctx, "yt-dlp."+stage, trace.WithAttributes(attrs...))
	defer span.End()
	output, err := yt.executor.Run(ctx, "yt-dlp", args...)
	if err != nil && span.IsRecording() {
		message := err.Error()
		// Some versions print the status on stdout. Inspect it only on failure.
		detail := message + "\n" + string(output)
		reason := commandDiagnostic(ctx, detail)
		span.SetAttributes(attribute.String("error.type", reason))
		if match := httpStatus.FindStringSubmatch(detail); match != nil {
			status, _ := strconv.Atoi(match[1])
			span.SetAttributes(attribute.Int("http.response.status_code", status))
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			span.SetAttributes(attribute.Int("process.exit.code", exit.ExitCode()))
		}
		span.SetStatus(codes.Error, reason)
	} else if err == nil {
		span.SetAttributes(attribute.Int("process.exit.code", 0))
	}
	return output, err
}

func commandDiagnostic(ctx context.Context, detail string) string {
	if err := ctx.Err(); errors.Is(err, context.Canceled) {
		return "canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	lower := strings.ToLower(detail)
	for _, signature := range []struct{ match, reason string }{
		{"too many requests", "rate_limited"}, {"http error 429", "rate_limited"},
		{"unable to download video subtitles", "subtitle_download_failed"},
		{"requested format is not available", "format_unavailable"},
		{"sign in to confirm", "sign_in_required"}, {"po token", "po_token_required"},
		{"challenge solving failed", "player_challenge_failed"}, {"video unavailable", "video_unavailable"},
		{"no space left", "disk_full"}, {"permission denied", "permission_denied"},
		{"executable file not found", "executable_missing"}, {"ffmpeg not found", "ffmpeg_missing"},
		{"unable to resolve", "dns_failure"}, {"name resolution", "dns_failure"},
		{"nodename nor servname", "dns_failure"}, {"timed out", "network_timeout"},
		{"connection reset", "connection_reset"}, {"http error", "http_error"},
	} {
		if strings.Contains(lower, signature.match) {
			return signature.reason
		}
	}
	return "process_failed"
}
