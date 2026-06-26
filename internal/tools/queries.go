package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterQueries registers query log tools.
func RegisterQueries(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_queries_search",
		mcp.WithTitleAnnotation("Search Query Log"),
		mcp.WithDescription("Search DNS query log with filters by domain, client, type, status, and time range. Returns 25 most recent by default with cursor pagination."),
		mcp.WithString("domain", mcp.Description("Domain filter (wildcards * supported).")),
		mcp.WithString("client_ip", mcp.Description("Client IP filter (wildcards supported).")),
		mcp.WithString("client_name", mcp.Description("Client hostname filter.")),
		mcp.WithString("upstream", mcp.Description("Upstream server filter.")),
		mcp.WithString("type", mcp.Description("Query type: A, AAAA, MX, etc.")),
		mcp.WithString("status", mcp.Description("Status: GRAVITY, FORWARDED, CACHE, etc.")),
		mcp.WithString("reply", mcp.Description("Reply type: NODATA, NXDOMAIN, IP, etc.")),
		mcp.WithString("dnssec", mcp.Description("DNSSEC status: SECURE, INSECURE, etc.")),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp.")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp.")),
		mcp.WithNumber("length", mcp.Description("Results per page (default 25, max 100).")),
		mcp.WithNumber("cursor", mcp.Description("Cursor from previous response for next page.")),
		detailParam,
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), queriesSearchHandler(r))

	addTool(s, r, mcp.NewTool("pihole_queries_suggestions",
		mcp.WithTitleAnnotation("Query Filter Suggestions"),
		mcp.WithDescription("Available filter values for pihole_queries_search: known domains, clients, types, statuses, and reply types."),
		mcp.WithReadOnlyHintAnnotation(true),
	), queriesSuggestionsHandler(r))
}

func queriesSearchHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		params := make(map[string]string)
		for _, key := range []string{"domain", "client_ip", "client_name", "upstream", "type", "status", "reply", "dnssec"} {
			if v := req.GetString(key, ""); v != "" {
				params[key] = v
			}
		}
		if v := req.GetFloat("from", 0); v > 0 {
			params["from"] = fmt.Sprintf("%.0f", v)
		}
		if v := req.GetFloat("until", 0); v > 0 {
			params["until"] = fmt.Sprintf("%.0f", v)
		}
		length := getCountCapped(req, "length", 25, 100)
		params["length"] = fmt.Sprintf("%d", length)
		if v := req.GetFloat("cursor", 0); v > 0 {
			params["cursor"] = fmt.Sprintf("%.0f", v)
		}

		path := "/queries" + format.QueryParams(params)
		var result pihole.QueriesResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("search queries", err), nil
		}

		detail := getDetail(req)

		if detail == "minimal" {
			text := fmt.Sprintf("%d of %d queries.", len(result.Queries), result.RecordsFiltered)
			if result.Cursor > 0 && len(result.Queries) < result.RecordsFiltered {
				text += fmt.Sprintf(" Next: cursor=%d", result.Cursor)
			}
			return mcp.NewToolResultText(text), nil
		}

		if wantCSV(req) {
			headers := []string{"Time", "Type", "Domain", "Status", "Client", "Upstream"}
			if detail == "full" {
				headers = append(headers, "DNSSEC", "ReplyType", "ReplyMs")
			}
			rows := make([][]string, 0, len(result.Queries))
			for _, q := range result.Queries {
				client := q.Client.IP
				if q.Client.Name != nil && *q.Client.Name != "" {
					client = *q.Client.Name
				}
				row := []string{format.Timestamp(q.Time), q.Type, q.Domain, q.Status, client, format.StringOr(q.Upstream, "")}
				if detail == "full" {
					row = append(row, format.StringOr(q.DNSSEC, ""), format.StringOr(q.Reply.Type, ""), fmt.Sprintf("%.1f", q.Reply.Time))
				}
				rows = append(rows, row)
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d of %d queries:**\n", len(result.Queries), result.RecordsFiltered)

		for _, q := range result.Queries {
			client := q.Client.IP
			if q.Client.Name != nil && *q.Client.Name != "" {
				client = *q.Client.Name
			}
			upstream := format.StringOr(q.Upstream, "-")
			fmt.Fprintf(&b, "- %s %s %s → %s (%s, %s)", format.Timestamp(q.Time), q.Type, q.Domain, q.Status, client, upstream)
			if detail == "full" {
				fmt.Fprintf(&b, " [dnssec=%s, reply=%s, %.1fms]",
					format.StringOr(q.DNSSEC, "N/A"), format.StringOr(q.Reply.Type, "N/A"), q.Reply.Time)
			}
			b.WriteString("\n")
		}

		if result.Cursor > 0 && len(result.Queries) < result.RecordsFiltered {
			fmt.Fprintf(&b, "\n_Next page: cursor=%d_\n", result.Cursor)
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func queriesSuggestionsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.QuerySuggestions
		if err := c.Get(ctx, "/queries/suggestions", &result); err != nil {
			return toolError("get query suggestions", err), nil
		}

		s := result.Suggestions
		var b strings.Builder
		writeSuggestionList(&b, "Types", s.Type)
		writeSuggestionList(&b, "Statuses", s.Status)
		writeSuggestionList(&b, "Replies", s.Reply)
		writeSuggestionList(&b, "DNSSEC", s.DNSSEC)
		writeSuggestionList(&b, "Upstreams", s.Upstream)
		writeSuggestionList(&b, "Clients", s.ClientIP)

		return mcp.NewToolResultText(b.String()), nil
	}
}

func writeSuggestionList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "**%s:** %s\n", label, strings.Join(items, ", "))
}
