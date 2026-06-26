package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// sensitiveKeys are redacted (case-insensitive substring match) from any map
// data before it is sent to the client as a log message.
var sensitiveKeys = []string{"password", "sid", "token", "authorization", "secret"}

// sendLog emits an MCP logging/message notification at the given level. The
// mcp-go SDK gates delivery by the client's configured log level, so handlers
// can log freely. It is a no-op when no MCP server is in context (e.g. unit
// tests that invoke handlers directly). Data is redacted of credentials first.
func sendLog(ctx context.Context, level mcp.LoggingLevel, logger string, data any) {
	srv := server.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	_ = srv.SendLogMessageToClient(ctx, mcp.NewLoggingMessageNotification(level, logger, redactCredentials(data)))
}

// redactCredentials recursively replaces the values of credential-bearing keys
// in maps with "[redacted]". Non-map values pass through unchanged.
func redactCredentials(data any) any {
	switch v := data.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			if isSensitiveKey(k) {
				out[k] = "[redacted]"
			} else {
				out[k] = redactCredentials(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = redactCredentials(val)
		}
		return out
	default:
		return data
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}
