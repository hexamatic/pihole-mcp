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
		mcp.WithDescription("Search DNS query log with filters by domain, client, type, status, and time range. Returns 25 most recent by default with cursor pagination. Set disk=true to search the long-term database for anything older than FTL's in-memory window."),
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
		mcp.WithNumber("length", mcp.Description("Results per page (default 25, max 100)."), mcp.Min(1), mcp.Max(100)),
		mcp.WithNumber("cursor", mcp.Description("Cursor from previous response for next page."), mcp.Min(0)),
		mcp.WithBoolean("disk", mcp.Description("Search the on-disk long-term database instead of FTL's in-memory window. Required for historical ranges; slower.")),
		detailParam,
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), queriesSearchHandler(r))

	addTool(s, r, mcp.NewTool("pihole_queries_suggestions",
		mcp.WithTitleAnnotation("Query Filter Suggestions"),
		mcp.WithDescription("Available filter values for pihole_queries_search: known domains, client addresses and names, upstreams, query types, statuses, reply types and DNSSEC states."),
		mcp.WithNumber("limit", mcp.Description("Maximum values to list per category (default 50)."), mcp.Min(1), mcp.Max(maxPageLimit)),
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
		length, err := getCountCapped(req, "length", 25, 1, 100)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		params["length"] = fmt.Sprintf("%d", length)
		if v := req.GetFloat("cursor", 0); v > 0 {
			params["cursor"] = fmt.Sprintf("%.0f", v)
		}
		// FTL only reads the long-term database when asked. Without this the
		// tool searched FTL's in-memory window whatever from/until said, so a
		// range older than that window returned zero rows and no explanation.
		disk := req.GetBool("disk", false)
		if disk {
			params["disk"] = "true"
		}

		path := "/queries" + format.QueryParams(params)
		var result pihole.QueriesResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("search queries", err), nil
		}

		if len(result.Queries) == 0 {
			return mcp.NewToolResultText(explainNoQueries(result, disk)), nil
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

		limit, err := getCountCapped(req, "limit", defaultSuggestionLimit, 1, maxPageLimit)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		s := result.Suggestions
		var b strings.Builder
		// Domains and client names are the two lists the tool's description
		// leads with and the two it never printed, so a caller asking what it
		// could filter by was told everything except that.
		writeSuggestionList(&b, "Domains", s.Domain, limit)
		writeSuggestionList(&b, "Clients", s.ClientIP, limit)
		writeSuggestionList(&b, "Client names", s.ClientName, limit)
		writeSuggestionList(&b, "Upstreams", s.Upstream, limit)
		writeSuggestionList(&b, "Types", s.Type, limit)
		writeSuggestionList(&b, "Statuses", s.Status, limit)
		writeSuggestionList(&b, "Replies", s.Reply, limit)
		writeSuggestionList(&b, "DNSSEC", s.DNSSEC, limit)

		return mcp.NewToolResultText(b.String()), nil
	}
}

// defaultSuggestionLimit bounds each suggestion category. The fixed lists
// (types, statuses, replies) are a couple of dozen values, but the domain and
// client lists grow with traffic and are unbounded on a busy Pi-hole.
const defaultSuggestionLimit = 50

func writeSuggestionList(b *strings.Builder, label string, items []string, limit int) {
	if len(items) == 0 {
		return
	}
	shown := items
	if len(shown) > limit {
		shown = shown[:limit]
	}
	fmt.Fprintf(b, "**%s:** %s", label, strings.Join(shown, ", "))
	if len(shown) < len(items) {
		fmt.Fprintf(b, " (%d of %d; raise limit for more)", len(shown), len(items))
	}
	b.WriteString("\n")
}

// explainNoQueries turns an empty search into an answer. A bare "0 of 0
// queries" is indistinguishable from a Pi-hole that answered nothing at all,
// and the usual cause is a range that predates what was searched: FTL keeps
// only a short window in memory and reads the long-term database on request.
// So say how far back the data actually goes, and how to reach further.
//
// FTL returns both floors on every search, so this costs no extra request.
func explainNoQueries(result pihole.QueriesResponse, disk bool) string {
	var b strings.Builder
	b.WriteString("**No queries matched.**")

	switch {
	case disk && result.EarliestTimestampDisk > 0:
		fmt.Fprintf(&b, " The long-term database only goes back to %s.", format.Timestamp(result.EarliestTimestampDisk))
	case disk:
		b.WriteString(" The long-term database holds nothing yet; FTL has not flushed any queries to disk.")
	case result.EarliestTimestamp > 0:
		fmt.Fprintf(&b, " FTL's in-memory window only goes back to %s.", format.Timestamp(result.EarliestTimestamp))
	}

	if !disk {
		b.WriteString(" Set disk=true to search the long-term database as well.")
	}
	b.WriteString("\n")

	return b.String()
}
