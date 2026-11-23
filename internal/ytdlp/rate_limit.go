package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rtzll/tldw/internal/tldw"
)

func terminalReadError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, tldw.ErrRateLimited)
}

func classifyReadError(ctx context.Context, ref tldw.ContentRef, output []byte, err error) error {
	if ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	if err == nil || terminalReadError(err) {
		return err
	}
	// CommandError includes stderr. Some yt-dlp versions report the 429 on stdout.
	details := string(output) + "\n" + err.Error()
	lower := strings.ToLower(details)
	if !strings.Contains(lower, "http error 429") && !strings.Contains(lower, "too many requests") {
		return err
	}
	if ref.IsX() {
		return fmt.Errorf("%w: %w", tldw.ErrRateLimited, err)
	}
	return &tldw.YouTubeRateLimitError{RetryAfter: retryAfter(details), Cause: err}
}

// yt-dlp usually prints only the status. Honor an HTTP Retry-After field
// when the command diagnostics include it, in either standard wire format.
var retryAfterHeader = regexp.MustCompile(`(?im)\bretry-after:[ \t]*([^\r\n]+)`)

func retryAfter(details string) time.Duration {
	for _, match := range retryAfterHeader.FindAllStringSubmatch(details, -1) {
		value := strings.TrimSpace(match[1])
		if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
			if seconds > uint64(math.MaxInt64/int64(time.Second)) {
				return time.Duration(math.MaxInt64)
			}
			return time.Duration(seconds) * time.Second
		}
		if date, err := http.ParseTime(value); err == nil {
			return max(0, time.Until(date))
		}
	}
	return 0
}
