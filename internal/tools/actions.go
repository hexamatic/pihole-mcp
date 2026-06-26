package tools

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterActions registers action tools (gravity update, restart, flush).
func RegisterActions(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_action_gravity_update",
		mcp.WithTitleAnnotation("Update Gravity"),
		mcp.WithDescription("Re-download all configured blocklists and rebuild the gravity database. Takes 30+ seconds. Run after adding or removing lists."),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), actionGravityHandler(r))

	addTool(s, r, mcp.NewTool("pihole_action_restart_dns",
		mcp.WithTitleAnnotation("Restart DNS"),
		mcp.WithDescription("Restart the FTL DNS resolver. Briefly interrupts DNS resolution for all clients on the network."),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), actionRestartDNSHandler(r))

	addTool(s, r, mcp.NewTool("pihole_action_flush_logs",
		mcp.WithTitleAnnotation("Flush Query Logs"),
		mcp.WithDescription("Permanently delete all DNS logs — purges last 24 hours from memory and database. Irreversible."),
		mcp.WithDestructiveHintAnnotation(true),
	), actionFlushLogsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_action_flush_network",
		mcp.WithTitleAnnotation("Flush Network Table"),
		mcp.WithDescription("Permanently delete all network device records and associated addresses. Devices will be re-discovered over time."),
		mcp.WithDestructiveHintAnnotation(true),
	), actionFlushNetworkHandler(r))
}

func actionGravityHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		prog := newProgressReporter(ctx, req)
		prog.report(0, 100, "Starting gravity update")
		sendLog(ctx, mcp.LoggingLevelInfo, "gravity", map[string]any{"instance": c.Name(), "event": "gravity_update_start"})

		resp, err := c.DoRaw(ctx, "POST", "/action/gravity", nil)
		if err != nil {
			sendLog(ctx, mcp.LoggingLevelError, "gravity", map[string]any{"instance": c.Name(), "event": "gravity_update_failed", "error": err.Error()})
			return toolError("start gravity update", err), nil
		}
		defer func() { _ = resp.Body.Close() }()

		// Pi-hole streams the gravity rebuild as it progresses. Read it line by
		// line so we can surface progress notifications, accumulating the full
		// output for the final result.
		var lines []string
		steps := 0.0
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			if isGravityLandmark(line) {
				steps++
				// Advance towards 90; the final 10 is reserved for completion.
				prog.report(min(90, steps*10), 100, strings.TrimSpace(line))
			}
		}
		if err := scanner.Err(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read gravity output: %v", err)), nil
		}

		prog.report(100, 100, "Gravity update complete")
		sendLog(ctx, mcp.LoggingLevelInfo, "gravity", map[string]any{"instance": c.Name(), "event": "gravity_update_complete"})
		return mcp.NewToolResultText(fmt.Sprintf("**Gravity update complete.**\n```\n%s\n```", strings.Join(lines, "\n"))), nil
	}
}

// isGravityLandmark recognises the notable phase markers in Pi-hole's gravity
// rebuild output so progress can advance roughly in step with the work.
func isGravityLandmark(line string) bool {
	for _, marker := range []string{"Pulling", "Storing", "Building", "Parsed", "gravity"} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func actionRestartDNSHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.ActionResponse
		if err := c.Post(ctx, "/action/restartdns", nil, &result); err != nil {
			return toolError("restart DNS", err), nil
		}

		sendLog(ctx, mcp.LoggingLevelNotice, "actions", map[string]any{"instance": c.Name(), "event": "dns_restarted"})
		return mcp.NewToolResultText("**DNS restarted** successfully."), nil
	}
}

func actionFlushLogsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		prog := newProgressReporter(ctx, req)
		prog.report(0, 1, "Flushing logs")

		var result pihole.ActionResponse
		if err := c.Post(ctx, "/action/flush/logs", nil, &result); err != nil {
			return toolError("flush logs", err), nil
		}

		prog.report(1, 1, "Logs flushed")
		sendLog(ctx, mcp.LoggingLevelNotice, "actions", map[string]any{"instance": c.Name(), "event": "logs_flushed"})
		return mcp.NewToolResultText("**Logs flushed.** Last 24 hours purged from memory and database."), nil
	}
}

func actionFlushNetworkHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		prog := newProgressReporter(ctx, req)
		prog.report(0, 1, "Flushing network table")

		var result pihole.ActionResponse
		if err := c.Post(ctx, "/action/flush/network", nil, &result); err != nil {
			return toolError("flush network table", err), nil
		}

		prog.report(1, 1, "Network table flushed")
		sendLog(ctx, mcp.LoggingLevelNotice, "actions", map[string]any{"instance": c.Name(), "event": "network_flushed"})
		return mcp.NewToolResultText("**Network table flushed.** All device records removed."), nil
	}
}
