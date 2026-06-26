package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterLogs registers log retrieval tools.
func RegisterLogs(s *server.MCPServer, r *pihole.Registry) {
	for _, l := range []struct {
		name, title, endpoint, desc string
	}{
		{"pihole_logs_dns", "DNS Log", "/logs/dnsmasq",
			"DNS resolver (dnsmasq) log. Use next_id for incremental polling to follow new entries."},
		{"pihole_logs_ftl", "FTL Log", "/logs/ftl",
			"FTL engine log — internal Pi-hole diagnostics, database operations, and resolver events."},
		{"pihole_logs_webserver", "Web Server Log", "/logs/webserver",
			"Web server access log — HTTP requests to the Pi-hole admin interface and API."},
	} {
		addTool(s, r, mcp.NewTool(l.name,
			mcp.WithTitleAnnotation(l.title),
			mcp.WithDescription(l.desc),
			mcp.WithNumber("next_id", mcp.Description("Only return lines after this ID (incremental polling).")),
			mcp.WithReadOnlyHintAnnotation(true),
		), logHandler(r, l.endpoint))
	}
}

func logHandler(r *pihole.Registry, endpoint string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path := endpoint
		if nextID := req.GetFloat("next_id", 0); nextID > 0 {
			path += fmt.Sprintf("?nextID=%.0f", nextID)
		}

		var result pihole.LogResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get logs", err), nil
		}

		if len(result.Log) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No log entries. Next ID: %d", result.NextID)), nil
		}

		shown := min(50, len(result.Log))
		var b strings.Builder
		fmt.Fprintf(&b, "**%d lines** (showing %d):\n", len(result.Log), shown)
		for _, entry := range result.Log[:shown] {
			fmt.Fprintf(&b, "- %s\n", entry.Message)
		}
		fmt.Fprintf(&b, "Next ID: %d", result.NextID)

		return mcp.NewToolResultText(b.String()), nil
	}
}
