package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterHistory registers activity history tools.
func RegisterHistory(s *server.MCPServer, c *pihole.Client) {
	addTool(s, mcp.NewTool("pihole_history_graph",
		mcp.WithDescription("Recent in-memory activity over time: total, cached, blocked, and forwarded per slot. Covers the last ~24 hours of FTL memory."),
		mcp.WithReadOnlyHintAnnotation(true),
	), historyGraphHandler(c))

	addTool(s, mcp.NewTool("pihole_history_clients",
		mcp.WithDescription("Recent in-memory per-client query activity for top N clients (last ~24 hours of FTL memory)."),
		mcp.WithNumber("count", mcp.Description("Max clients to return (default 10, 0=all).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), historyClientsHandler(c))

	addTool(s, mcp.NewTool("pihole_history_database",
		mcp.WithDescription("Long-term query activity from the Pi-hole database over a date range. Returns time-bucketed totals for total, cached, blocked, and forwarded queries."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp (default: 7 days ago).")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp (default: now).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), historyDatabaseHandler(c))

	addTool(s, mcp.NewTool("pihole_history_database_clients",
		mcp.WithDescription("Long-term per-client query activity from the Pi-hole database over a date range."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp (default: 7 days ago).")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp (default: now).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), historyDatabaseClientsHandler(c))
}

func historyGraphHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var result pihole.HistoryResponse
		if err := c.Get(ctx, "/history", &result); err != nil {
			return toolError("get history", err), nil
		}

		return mcp.NewToolResultText(formatHistorySummary(result)), nil
	}
}

func historyClientsHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		count := int(req.GetFloat("count", 10))

		params := make(map[string]string)
		if count > 0 {
			params["N"] = fmt.Sprintf("%d", count)
		}

		var result pihole.ClientHistoryResponse
		if err := c.Get(ctx, "/history/clients"+format.QueryParams(params), &result); err != nil {
			return toolError("get client history", err), nil
		}

		return mcp.NewToolResultText(formatClientHistorySummary(result)), nil
	}
}

func historyDatabaseHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		from, until := getTimeRange(req, 7*24*time.Hour)
		path := "/history/database" + format.QueryParams(map[string]string{
			"from":  from,
			"until": until,
		})

		var result pihole.HistoryResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get database history", err), nil
		}

		return mcp.NewToolResultText(formatHistorySummary(result)), nil
	}
}

func historyDatabaseClientsHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		from, until := getTimeRange(req, 7*24*time.Hour)
		path := "/history/database/clients" + format.QueryParams(map[string]string{
			"from":  from,
			"until": until,
		})

		var result pihole.ClientHistoryResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get database client history", err), nil
		}

		return mcp.NewToolResultText(formatClientHistorySummary(result)), nil
	}
}

func formatHistorySummary(r pihole.HistoryResponse) string {
	if len(r.History) == 0 {
		return "No history data available."
	}

	var totalQ, totalBlocked, totalCached, totalFwd int
	for _, h := range r.History {
		totalQ += h.Total
		totalBlocked += h.Blocked
		totalCached += h.Cached
		totalFwd += h.Forwarded
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%d data points** (%s to %s)\n",
		len(r.History),
		format.Timestamp(r.History[0].Timestamp),
		format.Timestamp(r.History[len(r.History)-1].Timestamp))
	fmt.Fprintf(&b, "Total: %s queries, %s blocked, %s cached, %s forwarded\n",
		format.Number(totalQ), format.Number(totalBlocked),
		format.Number(totalCached), format.Number(totalFwd))
	return b.String()
}

func formatClientHistorySummary(r pihole.ClientHistoryResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%d clients:**\n", len(r.Clients))
	for ip, info := range r.Clients {
		name := format.StringOr(info.Name, "")
		if name != "" {
			fmt.Fprintf(&b, "- %s (%s) — %s queries\n", name, ip, format.Number(info.Total))
		} else {
			fmt.Fprintf(&b, "- %s — %s queries\n", ip, format.Number(info.Total))
		}
	}
	return b.String()
}
