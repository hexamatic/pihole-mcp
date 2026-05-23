//go:build slim

package tools

import "github.com/mark3labs/mcp-go/server"

// withTracing is a passthrough in slim builds — the OpenTelemetry SDK is
// excluded entirely, so there's no tracer to start a span on. Equivalent
// to setting OTEL_EXPORTER_OTLP_ENDPOINT="" in default builds.
func withTracing(_ string, handler server.ToolHandlerFunc) server.ToolHandlerFunc {
	return handler
}
