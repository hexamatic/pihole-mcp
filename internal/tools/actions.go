package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterActions registers action tools (gravity update, restart, flush).
func RegisterActions(s *server.MCPServer, r *pihole.Registry) {
	// Not marked idempotent. Repeating the call downloads every list again,
	// and FTL before v6.8 starts a second rebuild alongside one still
	// running, so a client must not replay it after an unclear outcome.
	addTool(s, r, mcp.NewTool("pihole_action_gravity_update",
		mcp.WithTitleAnnotation("Update Gravity"),
		mcp.WithDescription("Re-download all configured blocklists and rebuild the gravity database. Takes from 30 seconds to several minutes and reports any list that failed. Run after adding or removing lists."),
		mcp.WithOpenWorldHintAnnotation(true),
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

// gravityIdleTimeout is how long the gravity stream may go without a line
// before this server stops waiting. pihole -g prints as it works, but a large
// list can take minutes to parse on a small board without printing anything,
// so this is generous. It replaces the request timeout, which covered the
// whole rebuild and cut every run longer than 30 seconds off as a failure.
// A variable so tests can shorten it.
var gravityIdleTimeout = 5 * time.Minute

// gravityFailedLine is the line FTL v6.8 appends to the stream when pihole -g
// exits non-zero. Earlier versions send the failure after the stream has
// ended, where no client can see it.
const gravityFailedLine = "Gravity failed"

func actionGravityHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		prog := newProgressReporter(ctx, req)
		prog.report(0, 100, "Starting gravity update")
		sendLog(ctx, mcp.LoggingLevelInfo, "gravity", map[string]any{"instance": c.Name(), "event": "gravity_update_start"})

		// Stop waiting if the stream goes quiet for too long, but never because
		// the rebuild as a whole is slow.
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		idle := time.AfterFunc(gravityIdleTimeout, cancel)
		defer idle.Stop()

		resp, err := c.DoStream(streamCtx, http.MethodPost, "/action/gravity", nil)
		if err != nil {
			sendLog(ctx, mcp.LoggingLevelError, "gravity", map[string]any{"instance": c.Name(), "event": "gravity_update_failed", "error": err.Error()})
			var apiErr *pihole.APIError
			if errors.As(err, &apiErr) && apiErr.Key == "gravity_running" {
				return mcp.NewToolResultError("A gravity update is already running on this Pi-hole, so Pi-hole refused to start another. " +
					"Wait a few minutes for it to finish rather than calling this again."), nil
			}
			return toolError("start gravity update", err), nil
		}
		defer func() { _ = resp.Body.Close() }()

		// Pi-hole streams the gravity rebuild as it progresses. Read it line by
		// line so we can surface progress notifications, accumulating the full
		// output for the final result.
		var lines, problems []string
		steps := 0.0
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			idle.Reset(gravityIdleTimeout)
			line := scanner.Text()
			lines = append(lines, line)
			if isGravityProblem(line) {
				problems = append(problems, strings.TrimSpace(line))
			}
			if isGravityLandmark(line) {
				steps++
				// Advance towards 90; the final 10 is reserved for completion.
				prog.report(min(90, steps*10), 100, strings.TrimSpace(line))
			}
		}
		output := strings.Join(lines, "\n")

		if err := scanner.Err(); err != nil {
			// The rebuild runs in its own session on the Pi-hole and carries on
			// whether or not anyone is reading, so a broken stream says nothing
			// about how it ends.
			sendLog(ctx, mcp.LoggingLevelWarning, "gravity", map[string]any{"instance": c.Name(), "event": "gravity_update_unknown", "error": err.Error()})
			return mcp.NewToolResultError(fmt.Sprintf(
				"Stopped receiving gravity output before the rebuild finished (%v). Pi-hole carries on with the rebuild on its own, "+
					"so it may still be running or may already have finished. Check pihole_info_messages in a few minutes "+
					"rather than starting another run.\n```\n%s\n```", err, output)), nil
		}

		if lastLine(lines) == gravityFailedLine {
			sendLog(ctx, mcp.LoggingLevelError, "gravity", map[string]any{"instance": c.Name(), "event": "gravity_update_failed"})
			return mcp.NewToolResultError(fmt.Sprintf(
				"**Gravity update failed.** Pi-hole reported that the rebuild did not complete.%s\n```\n%s\n```",
				problemSummary(problems), output)), nil
		}

		prog.report(100, 100, "Gravity update complete")
		sendLog(ctx, mcp.LoggingLevelInfo, "gravity", map[string]any{"instance": c.Name(), "event": "gravity_update_complete"})
		return mcp.NewToolResultText(fmt.Sprintf("**Gravity update complete.**%s\n```\n%s\n```", problemSummary(problems), output)), nil
	}
}

// isGravityProblem recognises a line pihole -g marks as a failure, such as a
// list it could not download. A run with such lines can still complete.
func isGravityProblem(line string) bool {
	return strings.Contains(line, "[✗]")
}

// problemSummary lists the failures pihole -g reported, ahead of the full
// output, so a list that failed to download is not buried in it.
func problemSummary(problems []string) string {
	if len(problems) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nPi-hole reported %d problem(s):\n", len(problems))
	for _, p := range problems {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	return b.String()
}

// lastLine returns the last non-blank line.
func lastLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
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
		sent := time.Now()
		var result pihole.ActionResponse
		if err := c.Post(ctx, "/action/restartdns", nil, &result); err != nil {
			return toolError("restart DNS", err), nil
		}

		// FTL answers before it restarts, so "restarted" was reported while
		// the restart had not yet begun. Wait until it is answering again.
		if err := c.AwaitRestart(ctx, sent); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Pi-hole accepted the restart, but %v. "+
				"Check that it is answering before relying on it.", err)), nil
		}
		sendLog(ctx, mcp.LoggingLevelNotice, "actions", map[string]any{"instance": c.Name(), "event": "dns_restarted"})
		return mcp.NewToolResultText("**DNS restarted.** Pi-hole is answering again."), nil
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
