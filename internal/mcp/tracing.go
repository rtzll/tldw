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
			if extra := req.GetExtra(); extra != nil {
				ctx = propagator.Extract(ctx, propagation.HeaderCarrier(extra.Header))
			}
			carrier := propagation.MapCarrier{}
			for _, key := range propagator.Fields() {
				if v, ok := params.Meta[key].(string); ok {
					carrier.Set(key, v)
				}
			}
			ctx = propagator.Extract(ctx, carrier)
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
			result, err := next(ctx, method, req)
			id := span.SpanContext().TraceID()
			if err != nil {
				span.SetStatus(codes.Error, "MCP protocol error")
				span.SetAttributes(attribute.String("error.type", "protocol_error"))
				if id.IsValid() {
					var wire *jsonrpc.Error
					if errors.As(err, &wire) {
						withID := *wire
						withID.Message = fmt.Sprintf("%s [trace_id=%s]", err.Error(), id)
						err = &withID
					} else {
						err = fmt.Errorf("%w [trace_id=%s]", err, id)
					}
				}
			} else if tool, ok := result.(*mcp.CallToolResult); ok && tool.IsError {
				span.SetStatus(codes.Error, "MCP tool error")
				span.SetAttributes(attribute.String("error.type", "tool_error"))
				if id.IsValid() {
					suffix := fmt.Sprintf(" [trace_id=%s]", id)
					if len(tool.Content) == 1 {
						if text, ok := tool.Content[0].(*mcp.TextContent); ok {
							copy := *text
							copy.Text += suffix
							tool.Content = []mcp.Content{&copy}
							return result, nil
						}
					}
					tool.Content = append(tool.Content, &mcp.TextContent{Text: suffix})
				}
			}
			return result, err
		}
	}
}
