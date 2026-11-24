package mcpserver

import (
	"net/http"
	"strconv"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Record arrival before protocol validation. A rejected request may never
// enter the tools/call middleware. Routes and methods have bounded values.
func traceHTTP(tracer trace.Tracer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Path
		if route != "/" && route != "/mcp" {
			route = "unmatched"
		}
		method := r.Method
		switch method {
		case http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodHead, http.MethodOptions:
		default:
			method = "_OTHER"
		}
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, method+" "+route, trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("http.request.method", method), attribute.String("http.route", route)))
		defer span.End()
		if id := span.SpanContext().TraceID(); id.IsValid() {
			w.Header().Set("X-Tldw-Trace-Id", id.String())
		}
		// Preserve the optional ResponseWriter interfaces used for SSE.
		metrics := httpsnoop.CaptureMetrics(next, w, r.WithContext(ctx))
		span.SetAttributes(attribute.Int("http.response.status_code", metrics.Code), attribute.Int64("http.response.body.size", metrics.Written))
		switch {
		case r.Context().Err() != nil:
			span.SetStatus(codes.Error, "request canceled")
			span.SetAttributes(attribute.String("error.type", "canceled"))
		case metrics.Code >= 400:
			span.SetStatus(codes.Error, http.StatusText(metrics.Code))
			span.SetAttributes(attribute.String("error.type", strconv.Itoa(metrics.Code)))
		}
	})
}
