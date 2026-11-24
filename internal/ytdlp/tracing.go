package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
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

const captionClientMarker = "tldw.caption.player_client="

// Only export a single recognized player name from the selected subtitle.
// Older yt-dlp versions and failures before selection may omit this field.
func captionPlayerClient(output []byte) string {
	for line := range bytes.Lines(output) {
		value, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte(captionClientMarker))
		if !ok {
			continue
		}
		var clients []string
		if err := json.Unmarshal(value, &clients); err == nil && len(clients) == 1 {
			switch clients[0] {
			case "web", "android", "tv_samsung", "visionos", "web_embedded", "web_creator", "web_safari":
				return clients[0]
			}
		}
	}
	return "unknown"
}

// run traces one actual subprocess, including failed attempts later recovered
// by the caller. Keep raw arguments and process output out of both exporters.
func (yt *Client) run(ctx context.Context, stage string, ref tldw.ContentRef, args []string, attrs ...attribute.KeyValue) ([]byte, error) {
	attrs = append(attrs, attribute.String("tldw.video.id", ref.CacheKey()))
	ctx, span := telemetry.Start(ctx, "yt-dlp."+stage, trace.WithAttributes(attrs...))
	defer telemetry.EndSpan(span)
	output, err := yt.executor.Run(ctx, "yt-dlp", args...)
	if stage == "captions" && !ref.IsX() && span.IsRecording() {
		span.SetAttributes(attribute.String("tldw.caption.player_client", captionPlayerClient(output)))
	}
	if err != nil && span.IsRecording() {
		detail := failureDiagnostic(err, output)
		reason := commandDiagnostic(ctx, stage, detail)
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

// A recovered warning (including a different HTTP status) can precede the
// terminal error. Some yt-dlp versions emit diagnostics on stdout.
func failureDiagnostic(err error, output []byte) string {
	detail := err.Error() + "\n" + string(output)
	last := ""
	for line := range strings.Lines(detail) {
		if _, message, ok := strings.Cut(line, "ERROR: "); ok {
			last = message
		}
	}
	if last != "" {
		return last
	}
	return detail
}

func emptyCaptionDiagnostic(detail string) bool {
	return strings.EqualFold(strings.TrimSpace(detail), "Did not get any data blocks")
}

func commandDiagnostic(ctx context.Context, stage, detail string) string {
	if err := ctx.Err(); errors.Is(err, context.Canceled) {
		return "canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if stage == "captions" && emptyCaptionDiagnostic(detail) {
		return "empty_caption_response"
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
