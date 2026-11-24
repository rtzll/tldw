package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// MCP metadata works on both transports. HTTP trace headers are a fallback;
// extract only W3C trace context, never arbitrary baggage or credentials.
func traceToolCalls(tracer trace.Tracer, transport string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if method != mcpMethodCallTool || !ok {
				return next(ctx, method, req)
			}
			propagator := propagation.TraceContext{}
			ingress := trace.SpanFromContext(ctx)
			if extra := req.GetExtra(); extra != nil && !ingress.SpanContext().IsValid() {
				ctx = propagator.Extract(ctx, propagation.HeaderCarrier(extra.Header))
			}
			carrier := propagation.MapCarrier{}
			for _, key := range propagator.Fields() {
				if v, ok := params.Meta[key].(string); ok {
					carrier.Set(key, v)
				}
			}
			ctx = propagator.Extract(ctx, carrier)
			parent := trace.SpanContextFromContext(ctx)
			// Unknown, user-supplied tool names must not enter exported attributes.
			name := params.Name
			switch name {
			case "get_youtube_metadata", "get_youtube_transcript", "transcribe_video_paid":
			default:
				name = "unknown"
			}
			ctx, span := tracer.Start(ctx, "tools/call "+name, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
				attribute.String("mcp.method.name", method), attribute.String("gen_ai.tool.name", name), attribute.String("tldw.transport", transport)))
			defer span.End()
			// A caller can send its own MCP parent independently of the HTTP
			// headers. Keep both sides navigable when the parents differ.
			if ingress.IsRecording() && !parent.Equal(ingress.SpanContext()) {
				span.AddLink(trace.Link{SpanContext: ingress.SpanContext()})
				ingress.AddLink(trace.Link{SpanContext: span.SpanContext()})
			}
			result, err := next(ctx, method, req)
			id := span.SpanContext().TraceID()
			if err != nil {
				span.SetStatus(codes.Error, "MCP protocol error")
				span.SetAttributes(attribute.String("error.type", "protocol_error"))
				return result, protocolErrorWithTraceID(err, id)
			}
			if tool, ok := result.(*mcp.CallToolResult); ok && tool.IsError {
				span.SetStatus(codes.Error, "MCP tool error")
				span.SetAttributes(attribute.String("error.type", "tool_error"))
				addToolTraceID(tool, id)
			}
			return result, nil
		}
	}
}

func protocolErrorWithTraceID(err error, id trace.TraceID) error {
	if !id.IsValid() {
		return err
	}
	// Preserve both the JSON-RPC code and structured data when adding context.
	var wire *jsonrpc.Error
	if errors.As(err, &wire) {
		withID := *wire
		withID.Message = fmt.Sprintf("%s [trace_id=%s]", err.Error(), id)
		return &withID
	}
	return fmt.Errorf("%w [trace_id=%s]", err, id)
}

func addToolTraceID(tool *mcp.CallToolResult, id trace.TraceID) {
	if !id.IsValid() {
		return
	}
	suffix := fmt.Sprintf("[trace_id=%s]", id)
	if len(tool.Content) == 1 {
		if text, ok := tool.Content[0].(*mcp.TextContent); ok {
			annotated := *text
			annotated.Text += " " + suffix
			tool.Content = []mcp.Content{&annotated}
			return
		}
	}
	tool.Content = append(tool.Content, &mcp.TextContent{Text: suffix})
}
