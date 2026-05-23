//go:build !slim

package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("pihole-mcp/tools")

// withTracing wraps a tool handler with an OpenTelemetry span.
// When tracing is not configured, the overhead is negligible (noop tracer).
func withTracing(name string, handler server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, span := tracer.Start(ctx, "tool/"+name,
			trace.WithAttributes(attribute.String("tool.name", name)),
		)
		defer span.End()

		result, err := handler(ctx, req)

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if result != nil && result.IsError {
			span.SetStatus(codes.Error, "tool returned error")
		}

		return result, err
	}
}
