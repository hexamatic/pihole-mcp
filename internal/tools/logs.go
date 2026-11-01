package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	// defaultLogLines is how many lines a log tool shows unless asked.
	defaultLogLines = 50
	// maxLogLines is FTL's buffer: it keeps the last 512 lines of each log
	// (LOG_SIZE in src/log.h), so no request returns more.
	maxLogLines = 512
)

// logLevels are FTL's syslog priority names, most severe first, as it tags
// the lines of its own log (src/log.c). dnsmasq and web server lines carry
// no priority.
var logLevels = []string{"EMERG", "ALERT", "CRIT", "ERROR", "WARNING"}

// RegisterLogs registers log retrieval tools.
func RegisterLogs(s *server.MCPServer, r *pihole.Registry) {
	for _, l := range []struct {
		name, title, endpoint, desc string
		levels                      bool
	}{
		{"pihole_logs_dns", "DNS Log", "/logs/dnsmasq",
			"Newest lines of the DNS resolver (dnsmasq) log. Use next_id to fetch only lines added since an earlier call.", false},
		{"pihole_logs_ftl", "FTL Log", "/logs/ftl",
			"Newest lines of the FTL engine log: Pi-hole's own diagnostics, database operations and resolver events. Filter to warnings or errors with level.", true},
		{"pihole_logs_webserver", "Web Server Log", "/logs/webserver",
			"Newest lines of the web server log: requests to the Pi-hole admin interface and API.", false},
	} {
		opts := []mcp.ToolOption{
			mcp.WithTitleAnnotation(l.title),
			mcp.WithDescription(l.desc),
			mcp.WithNumber("lines", mcp.Description(fmt.Sprintf("How many of the newest lines to show (default %d). Pi-hole keeps the last %d.", defaultLogLines, maxLogLines)),
				mcp.Min(1), mcp.Max(maxLogLines)),
			mcp.WithNumber("next_id", mcp.Description("Only return lines added after this ID, as given by the previous call (incremental polling).")),
		}
		if l.levels {
			opts = append(opts, mcp.WithString("level", mcp.Enum("all", "warning", "error"),
				mcp.Description("Only show lines at this severity or worse: warning includes errors (default all).")))
		}
		opts = append(opts, mcp.WithReadOnlyHintAnnotation(true))
		addTool(s, r, mcp.NewTool(l.name, opts...), logHandler(r, l.endpoint))
	}
}

// logHandler shows the newest lines of one of FTL's logs.
//
// FTL returns the lines oldest first. The handler used to show the first 50
// and report the cursor past the last line, so the newest lines, the ones a
// caller is usually after, were never shown, and polling from the cursor
// skipped everything after the 50th line for good.
func logHandler(r *pihole.Registry, endpoint string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		lines := req.GetInt("lines", defaultLogLines)
		if err := validateIntRange("lines", lines, 1, maxLogLines); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		level := req.GetString("level", "all")
		var keep []string
		switch level {
		case "all":
		case "warning":
			keep = logLevels
		case "error":
			keep = logLevels[:slices.Index(logLevels, "ERROR")+1]
		default:
			return mcp.NewToolResultError(`Invalid level: use "all", "warning" or "error"`), nil
		}

		path := endpoint
		if nextID := req.GetFloat("next_id", 0); nextID > 0 {
			path += fmt.Sprintf("?nextID=%.0f", nextID)
		}

		var result pihole.LogResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get logs", err), nil
		}

		entries := result.Log
		if keep != nil {
			entries = slices.DeleteFunc(slices.Clone(entries), func(e pihole.LogEntry) bool {
				return e.Priority == nil || !slices.Contains(keep, *e.Priority)
			})
		}
		next := fmt.Sprintf("Next ID: %d (pass it as next_id to fetch only newer lines).", result.NextID)
		if len(entries) == 0 {
			what := "No log lines"
			if keep != nil {
				what = fmt.Sprintf("No lines at %s level or worse among the %d returned", level, len(result.Log))
			}
			return mcp.NewToolResultText(what + ". " + next), nil
		}

		shown := entries[max(0, len(entries)-lines):]
		var b strings.Builder
		if len(shown) < len(entries) {
			fmt.Fprintf(&b, "**%d lines**, showing the newest %d. Pass lines=%d to see them all.\n",
				len(entries), len(shown), len(entries))
		} else {
			fmt.Fprintf(&b, "**%d lines:**\n", len(entries))
		}
		for _, e := range shown {
			fmt.Fprintf(&b, "- %s ", format.Clock(e.Timestamp))
			if p := deref(e.Priority); p != "" {
				b.WriteString(p + " ")
			}
			b.WriteString(e.Message + "\n")
		}
		b.WriteString(next)
		return mcp.NewToolResultText(b.String()), nil
	}
}
