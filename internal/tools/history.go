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
func RegisterHistory(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_history_graph",
		mcp.WithTitleAnnotation("Activity Graph"),
		mcp.WithDescription("In-memory query activity (FTL memory, last ~24h): total/cached/blocked/forwarded per slot. For arbitrary date ranges, use pihole_history_database."),
		mcp.WithReadOnlyHintAnnotation(true),
	), historyGraphHandler(r))

	addTool(s, r, mcp.NewTool("pihole_history_clients",
		mcp.WithTitleAnnotation("Per-Client Activity"),
		mcp.WithDescription("In-memory per-client query activity (FTL memory, last ~24h). For arbitrary date ranges, use pihole_history_database_clients."),
		mcp.WithNumber("count", mcp.Description("Max clients to return (default 10, 0=all).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), historyClientsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_history_database",
		mcp.WithTitleAnnotation("Long-Term Activity"),
		mcp.WithDescription("Long-term query activity from the FTL database (durable, arbitrary date range). Returns time-bucketed totals for total/cached/blocked/forwarded queries."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp (default: 7 days ago).")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp (default: now).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), historyDatabaseHandler(r))

	addTool(s, r, mcp.NewTool("pihole_history_database_clients",
		mcp.WithTitleAnnotation("Long-Term Per-Client Activity"),
		mcp.WithDescription("Long-term per-client query activity from the FTL database (durable, arbitrary date range)."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp (default: 7 days ago).")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp (default: now).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), historyDatabaseClientsHandler(r))
}

func historyGraphHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.HistoryResponse
		if err := c.Get(ctx, "/history", &result); err != nil {
			return toolError("get history", err), nil
		}

		return mcp.NewToolResultText(formatHistorySummary(result)), nil
	}
}

func historyClientsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
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

func historyDatabaseHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
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

func historyDatabaseClientsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
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
