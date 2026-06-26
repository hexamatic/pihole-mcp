package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterStats registers statistics and metrics tools.
func RegisterStats(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_stats_summary",
		mcp.WithTitleAnnotation("Statistics Summary"),
		mcp.WithDescription("Overview of Pi-hole activity: queries, blocking rate, active clients, gravity size, and top query types. Start here for a health check."),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithOutputSchema[StatsSummaryOutput](),
	), statsSummaryHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_top_domains",
		mcp.WithTitleAnnotation("Top Domains"),
		mcp.WithDescription("Top queried or top blocked domains ranked by count. Returns 10 by default, max 50."),
		mcp.WithBoolean("blocked", mcp.Description("True for top blocked, false/omit for top permitted.")),
		mcp.WithNumber("count", mcp.Description("Number of results (default 10, max 50).")),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithOutputSchema[TopListOutput](),
	), statsTopDomainsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_top_clients",
		mcp.WithTitleAnnotation("Top Clients"),
		mcp.WithDescription("Most active network clients ranked by query count. Use blocked=true for clients with most blocked queries."),
		mcp.WithBoolean("blocked", mcp.Description("True for most-blocked clients.")),
		mcp.WithNumber("count", mcp.Description("Number of results (default 10, max 50).")),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithOutputSchema[TopListOutput](),
	), statsTopClientsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_upstreams",
		mcp.WithTitleAnnotation("Upstream Performance"),
		mcp.WithDescription("Upstream DNS server performance: query counts, average response time, and standard deviation per server."),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), statsUpstreamsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_query_types",
		mcp.WithTitleAnnotation("Query Types"),
		mcp.WithDescription("Distribution of DNS query types (A, AAAA, MX, PTR, HTTPS, etc.) with counts."),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), statsQueryTypesHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_recent_blocked",
		mcp.WithTitleAnnotation("Recently Blocked"),
		mcp.WithDescription("Most recently blocked domains — useful for spotting new tracking domains or false positives in real-time."),
		mcp.WithNumber("count", mcp.Description("Number of domains (default 10).")),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), statsRecentBlockedHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_database",
		mcp.WithTitleAnnotation("Long-Term Statistics"),
		mcp.WithDescription("Long-term database statistics for a time range. Returns totals for queries, blocked, and clients."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp.")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), statsDatabaseHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_database_top_domains",
		mcp.WithTitleAnnotation("Long-Term Top Domains"),
		mcp.WithDescription("Top queried or blocked domains from the long-term database for a date range. Returns 10 by default, max 50."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp.")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp.")),
		mcp.WithNumber("count", mcp.Description("Number of results (default 10, max 50).")),
		mcp.WithBoolean("blocked", mcp.Description("True for top blocked, false/omit for top permitted.")),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), statsDatabaseTopDomainsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_database_top_clients",
		mcp.WithTitleAnnotation("Long-Term Top Clients"),
		mcp.WithDescription("Most active clients from the long-term database for a date range. Use blocked=true for clients with most blocked queries."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp.")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp.")),
		mcp.WithNumber("count", mcp.Description("Number of results (default 10, max 50).")),
		mcp.WithBoolean("blocked", mcp.Description("True for most-blocked clients.")),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), statsDatabaseTopClientsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_database_upstreams",
		mcp.WithTitleAnnotation("Long-Term Upstreams"),
		mcp.WithDescription("Historical upstream DNS server performance for a date range: query counts and average response times."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp.")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp.")),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), statsDatabaseUpstreamsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_stats_database_query_types",
		mcp.WithTitleAnnotation("Long-Term Query Types"),
		mcp.WithDescription("Historical distribution of DNS query types (A, AAAA, MX, etc.) for a date range."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp.")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), statsDatabaseQueryTypesHandler(r))
}

func statsSummaryHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var stats pihole.StatsSummary
		if err := c.Get(ctx, "/stats/summary", &stats); err != nil {
			return toolError("get stats", err), nil
		}

		q := stats.Queries
		detail := getDetail(req)

		output := StatsSummaryOutput{
			TotalQueries:     q.Total,
			BlockedQueries:   q.Blocked,
			PercentBlocked:   q.PercentBlocked,
			CachedQueries:    q.Cached,
			ForwardedQueries: q.Forwarded,
			ActiveClients:    stats.Clients.Active,
			TotalClients:     stats.Clients.Total,
			GravityDomains:   stats.Gravity.DomainsBeingBlocked,
		}

		if detail == "minimal" {
			textOutput := fmt.Sprintf(
				"Queries: %s | Blocked: %s (%s) | Clients: %d | Gravity: %s domains",
				format.Number(q.Total), format.Number(q.Blocked), format.Percent(q.PercentBlocked),
				stats.Clients.Active, format.Number(stats.Gravity.DomainsBeingBlocked))
			return mcp.NewToolResultStructured(output, textOutput), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**Queries:** %s total, %s blocked (%s), %s cached, %s forwarded\n",
			format.Number(q.Total), format.Number(q.Blocked), format.Percent(q.PercentBlocked),
			format.Number(q.Cached), format.Number(q.Forwarded))
		fmt.Fprintf(&b, "**Clients:** %d active of %d total\n", stats.Clients.Active, stats.Clients.Total)
		fmt.Fprintf(&b, "**Gravity:** %s domains", format.Number(stats.Gravity.DomainsBeingBlocked))
		if stats.Gravity.LastUpdate > 0 {
			fmt.Fprintf(&b, " (updated %s)", format.Timestamp(float64(stats.Gravity.LastUpdate)))
		}
		b.WriteString("\n")

		if len(q.Types) > 0 {
			sorted := sortMapByValue(q.Types)
			limit := 5
			if detail == "full" {
				limit = len(sorted)
			}
			b.WriteString("**Query types:** ")
			parts := make([]string, 0, limit)
			for _, kv := range sorted[:min(limit, len(sorted))] {
				if kv.Value > 0 || detail == "full" {
					parts = append(parts, fmt.Sprintf("%s=%s", kv.Key, format.Number(kv.Value)))
				}
			}
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString("\n")
		}

		if detail == "full" {
			if len(q.Status) > 0 {
				sorted := sortMapByValue(q.Status)
				b.WriteString("**Status breakdown:** ")
				parts := make([]string, 0, len(sorted))
				for _, kv := range sorted {
					if kv.Value > 0 {
						parts = append(parts, fmt.Sprintf("%s=%s", kv.Key, format.Number(kv.Value)))
					}
				}
				b.WriteString(strings.Join(parts, ", "))
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "**Unique domains:** %s | **Frequency:** %.1f queries/sec\n",
				format.Number(q.UniqueDomains), q.Frequency)
		}

		return mcp.NewToolResultStructured(output, b.String()), nil
	}
}

func statsTopDomainsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		count := getCountCapped(req, "count", 10, 50)
		blocked := req.GetBool("blocked", false)

		path := fmt.Sprintf("/stats/top_domains?count=%d&blocked=%t", count, blocked)
		var result pihole.TopItems
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get top domains", err), nil
		}

		headers := []string{"Rank", "Domain", "Queries"}
		rows := make([][]string, 0, len(result.Domains))
		items := make([]TopItemOutput, 0, len(result.Domains))
		for i, d := range result.Domains {
			rows = append(rows, []string{fmt.Sprintf("%d", i+1), d.Domain, format.Number(d.Count)})
			items = append(items, TopItemOutput{Name: d.Domain, Count: d.Count})
		}
		output := TopListOutput{Items: items, Count: len(items), Blocked: blocked}

		if wantCSV(req) {
			return mcp.NewToolResultStructured(output, format.CSV(headers, rows)), nil
		}

		label := "permitted"
		if blocked {
			label = "blocked"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "**Top %s domains:**\n", label)
		for i, d := range result.Domains {
			fmt.Fprintf(&b, "%d. %s — %s\n", i+1, d.Domain, format.Number(d.Count))
		}
		return mcp.NewToolResultStructured(output, b.String()), nil
	}
}

func statsTopClientsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		count := getCountCapped(req, "count", 10, 50)
		blocked := req.GetBool("blocked", false)

		path := fmt.Sprintf("/stats/top_clients?count=%d&blocked=%t", count, blocked)
		var result pihole.TopItems
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get top clients", err), nil
		}

		headers := []string{"Rank", "IP", "Name", "Queries"}
		rows := make([][]string, 0, len(result.Clients))
		items := make([]TopItemOutput, 0, len(result.Clients))
		for i, cl := range result.Clients {
			rows = append(rows, []string{fmt.Sprintf("%d", i+1), cl.IP, cl.Name, format.Number(cl.Count)})
			id := cl.Name
			if id == "" {
				id = cl.IP
			}
			items = append(items, TopItemOutput{Name: id, Count: cl.Count})
		}
		output := TopListOutput{Items: items, Count: len(items), Blocked: blocked}

		if wantCSV(req) {
			return mcp.NewToolResultStructured(output, format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		b.WriteString("**Top clients:**\n")
		for i, cl := range result.Clients {
			name := cl.Name
			if name == "" {
				name = cl.IP
			} else {
				name = fmt.Sprintf("%s (%s)", cl.Name, cl.IP)
			}
			fmt.Fprintf(&b, "%d. %s — %s\n", i+1, name, format.Number(cl.Count))
		}
		return mcp.NewToolResultStructured(output, b.String()), nil
	}
}

func statsUpstreamsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.Upstreams
		if err := c.Get(ctx, "/stats/upstreams", &result); err != nil {
			return toolError("get upstreams", err), nil
		}

		if wantCSV(req) {
			headers := []string{"Name", "IP", "Port", "Queries", "AvgResponseMs"}
			rows := make([][]string, 0, len(result.Upstreams))
			for _, u := range result.Upstreams {
				rows = append(rows, []string{
					format.StringOr(u.Name, ""),
					format.StringOr(u.IP, ""),
					format.Number(u.Port),
					format.Number(u.Count),
					format.ResponseTime(u.Statistics.Response),
				})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**Forwarded:** %s of %s total queries\n",
			format.Number(result.ForwardedQueries), format.Number(result.TotalQueries))
		for _, u := range result.Upstreams {
			name := format.StringOr(u.Name, format.StringOr(u.IP, "cache"))
			fmt.Fprintf(&b, "- %s:%d — %s queries, avg %s\n",
				name, u.Port, format.Number(u.Count), format.ResponseTime(u.Statistics.Response))
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

func statsQueryTypesHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.QueryTypes
		if err := c.Get(ctx, "/stats/query_types", &result); err != nil {
			return toolError("get query types", err), nil
		}

		sorted := sortMapByValue(result.Types)

		if wantCSV(req) {
			headers := []string{"Type", "Count"}
			rows := make([][]string, 0, len(sorted))
			for _, kv := range sorted {
				rows = append(rows, []string{kv.Key, format.Number(kv.Value)})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		b.WriteString("**Query types:**\n")
		for _, kv := range sorted {
			if kv.Value > 0 {
				fmt.Fprintf(&b, "- %s: %s\n", kv.Key, format.Number(kv.Value))
			}
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

func statsRecentBlockedHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		count := int(req.GetFloat("count", 10))
		path := fmt.Sprintf("/stats/recent_blocked?count=%d", count)

		var result pihole.RecentBlocked
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get recent blocked", err), nil
		}

		if len(result.Blocked) == 0 {
			return mcp.NewToolResultText("No recently blocked domains."), nil
		}

		if wantCSV(req) {
			headers := []string{"Rank", "Domain"}
			rows := make([][]string, 0, len(result.Blocked))
			for i, domain := range result.Blocked {
				rows = append(rows, []string{format.Number(i + 1), domain})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		b.WriteString("**Recently blocked:**\n")
		for i, domain := range result.Blocked {
			fmt.Fprintf(&b, "%d. %s\n", i+1, domain)
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

func statsDatabaseHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		from, until := getTimeRange(req, 24*time.Hour)

		params := map[string]string{
			"from":  from,
			"until": until,
		}

		path := "/stats/database/summary" + format.QueryParams(params)
		var result pihole.DatabaseSummary
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get database stats", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf(
			"**Database summary:** %s queries, %s blocked (%s), %d clients",
			format.Number(result.SumQueries), format.Number(result.SumBlocked),
			format.Percent(result.PercentBlocked), result.TotalClients)), nil
	}
}

func statsDatabaseTopDomainsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		from, until := getTimeRange(req, 24*time.Hour)
		count := getCountCapped(req, "count", 10, 50)
		blocked := req.GetBool("blocked", false)

		params := map[string]string{
			"from":    from,
			"until":   until,
			"count":   fmt.Sprintf("%d", count),
			"blocked": fmt.Sprintf("%t", blocked),
		}

		path := "/stats/database/top_domains" + format.QueryParams(params)
		var result pihole.TopItems
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get database top domains", err), nil
		}

		headers := []string{"Rank", "Domain", "Queries"}
		rows := make([][]string, 0, len(result.Domains))
		for i, d := range result.Domains {
			rows = append(rows, []string{fmt.Sprintf("%d", i+1), d.Domain, format.Number(d.Count)})
		}

		if wantCSV(req) {
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		label := "permitted"
		if blocked {
			label = "blocked"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "**Top %s domains (database):**\n", label)
		for i, d := range result.Domains {
			fmt.Fprintf(&b, "%d. %s — %s\n", i+1, d.Domain, format.Number(d.Count))
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

func statsDatabaseTopClientsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		from, until := getTimeRange(req, 24*time.Hour)
		count := getCountCapped(req, "count", 10, 50)
		blocked := req.GetBool("blocked", false)

		params := map[string]string{
			"from":    from,
			"until":   until,
			"count":   fmt.Sprintf("%d", count),
			"blocked": fmt.Sprintf("%t", blocked),
		}

		path := "/stats/database/top_clients" + format.QueryParams(params)
		var result pihole.TopItems
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get database top clients", err), nil
		}

		headers := []string{"Rank", "IP", "Name", "Queries"}
		rows := make([][]string, 0, len(result.Clients))
		for i, cl := range result.Clients {
			rows = append(rows, []string{fmt.Sprintf("%d", i+1), cl.IP, cl.Name, format.Number(cl.Count)})
		}

		if wantCSV(req) {
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		b.WriteString("**Top clients (database):**\n")
		for i, cl := range result.Clients {
			name := cl.Name
			if name == "" {
				name = cl.IP
			} else {
				name = fmt.Sprintf("%s (%s)", cl.Name, cl.IP)
			}
			fmt.Fprintf(&b, "%d. %s — %s\n", i+1, name, format.Number(cl.Count))
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

func statsDatabaseUpstreamsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		from, until := getTimeRange(req, 24*time.Hour)

		params := map[string]string{
			"from":  from,
			"until": until,
		}

		path := "/stats/database/upstreams" + format.QueryParams(params)
		var result pihole.Upstreams
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get database upstreams", err), nil
		}

		if wantCSV(req) {
			headers := []string{"Name", "IP", "Port", "Queries", "AvgResponseMs"}
			rows := make([][]string, 0, len(result.Upstreams))
			for _, u := range result.Upstreams {
				rows = append(rows, []string{
					format.StringOr(u.Name, ""),
					format.StringOr(u.IP, ""),
					format.Number(u.Port),
					format.Number(u.Count),
					format.ResponseTime(u.Statistics.Response),
				})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**Forwarded (database):** %s of %s total queries\n",
			format.Number(result.ForwardedQueries), format.Number(result.TotalQueries))
		for _, u := range result.Upstreams {
			name := format.StringOr(u.Name, format.StringOr(u.IP, "cache"))
			fmt.Fprintf(&b, "- %s:%d — %s queries, avg %s\n",
				name, u.Port, format.Number(u.Count), format.ResponseTime(u.Statistics.Response))
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

func statsDatabaseQueryTypesHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		from, until := getTimeRange(req, 24*time.Hour)

		params := map[string]string{
			"from":  from,
			"until": until,
		}

		path := "/stats/database/query_types" + format.QueryParams(params)
		var result pihole.QueryTypes
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get database query types", err), nil
		}

		sorted := sortMapByValue(result.Types)
		var b strings.Builder
		b.WriteString("**Query types (database):**\n")
		for _, kv := range sorted {
			if kv.Value > 0 {
				fmt.Fprintf(&b, "- %s: %s\n", kv.Key, format.Number(kv.Value))
			}
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

type kv struct {
	Key   string
	Value int
}

func sortMapByValue(m map[string]int) []kv {
	sorted := make([]kv, 0, len(m))
	for k, v := range m {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})
	return sorted
}
