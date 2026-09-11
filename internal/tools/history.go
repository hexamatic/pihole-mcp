package tools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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
		mcp.WithDescription("In-memory query activity (FTL memory, last ~24h): total/cached/blocked/forwarded per time slot. detail=full or format=csv returns every slot. For arbitrary date ranges, use pihole_history_database."),
		detailParam,
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), historyGraphHandler(r))

	addTool(s, r, mcp.NewTool("pihole_history_clients",
		mcp.WithTitleAnnotation("Per-Client Activity"),
		mcp.WithDescription("In-memory per-client query activity (FTL memory, last ~24h), busiest client first. detail=full returns the per-slot time series. For arbitrary date ranges, use pihole_history_database_clients."),
		mcp.WithNumber("count", mcp.Description("Max clients to return (default 10, 0 for all)."), mcp.Min(0), mcp.Max(maxPageLimit)),
		detailParam,
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), historyClientsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_history_database",
		mcp.WithTitleAnnotation("Long-Term Activity"),
		mcp.WithDescription("Long-term query activity from the FTL database as a time series: one bucket per slot across the range. Use pihole_stats_database instead for a single set of totals over the whole range."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp (default: 7 days ago).")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp (default: now).")),
		detailParam,
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), historyDatabaseHandler(r))

	addTool(s, r, mcp.NewTool("pihole_history_database_clients",
		mcp.WithTitleAnnotation("Long-Term Per-Client Activity"),
		mcp.WithDescription("Long-term per-client query activity from the FTL database (durable, arbitrary date range), busiest client first. detail=full returns the per-slot time series."),
		mcp.WithNumber("from", mcp.Description("Start Unix timestamp (default: 7 days ago).")),
		mcp.WithNumber("until", mcp.Description("End Unix timestamp (default: now).")),
		detailParam,
		formatParam,
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

		return mcp.NewToolResultText(formatHistorySummary(result, getDetail(req), wantCSV(req))), nil
	}
}

func historyClientsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		count, err := getCountCapped(req, "count", 10, 0, maxPageLimit)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		params := make(map[string]string)
		if count > 0 {
			params["N"] = fmt.Sprintf("%d", count)
		}

		var result pihole.ClientHistoryResponse
		if err := c.Get(ctx, "/history/clients"+format.QueryParams(params), &result); err != nil {
			return toolError("get client history", err), nil
		}

		return mcp.NewToolResultText(formatClientHistorySummary(result, getDetail(req), wantCSV(req))), nil
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

		return mcp.NewToolResultText(formatHistorySummary(result, getDetail(req), wantCSV(req))), nil
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

		return mcp.NewToolResultText(formatClientHistorySummary(result, getDetail(req), wantCSV(req))), nil
	}
}

func formatHistorySummary(r pihole.HistoryResponse, detail string, csv bool) string {
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

	summary := fmt.Sprintf("**%d data points** (%s to %s)\nTotal: %s queries, %s blocked, %s cached, %s forwarded\n",
		len(r.History),
		format.Timestamp(r.History[0].Timestamp),
		format.Timestamp(r.History[len(r.History)-1].Timestamp),
		format.Number(totalQ), format.Number(totalBlocked),
		format.Number(totalCached), format.Number(totalFwd))

	if detail == "minimal" {
		return summary
	}

	// The per-slot series is the data these tools exist to return, and the
	// summary above throws all of it away. It is roughly 3.5 KB for a full
	// 145-slot day, so it is opt-in rather than the default.
	if !csv && detail != "full" {
		return summary
	}

	headers := []string{"Time", "Total", "Cached", "Blocked", "Forwarded"}
	rows := make([][]string, 0, len(r.History))
	for _, h := range r.History {
		rows = append(rows, []string{
			format.Timestamp(h.Timestamp),
			strconv.Itoa(h.Total), strconv.Itoa(h.Cached),
			strconv.Itoa(h.Blocked), strconv.Itoa(h.Forwarded),
		})
	}

	if csv {
		return format.CSV(headers, rows)
	}
	return summary + "\n" + format.CSV(headers, rows)
}

// clientTotal pairs a client key with the metadata needed to rank and label it.
type clientTotal struct {
	key   string
	name  string
	total int
}

// rankClients orders clients by query count, highest first, breaking ties on
// the client key. Ranging over the map directly meant two identical calls
// rendered the same clients in a different order, which reads as movement in
// the data when nothing has changed.
//
// When FTL declares no clients at all the series is the only source there is:
// against v6.7 the in-memory endpoint answers with an empty client map while
// bucketing every slot under a key of its own ("others"), and a renderer
// reading only the map reported no clients while holding a full day of data.
// The declared map wins whenever it has anything in it, because the database
// endpoint keys its buckets by database row id rather than by address, and
// those ids are not clients to be named.
func rankClients(r pihole.ClientHistoryResponse) []clientTotal {
	out := make([]clientTotal, 0, len(r.Clients))
	if len(r.Clients) > 0 {
		for key, info := range r.Clients {
			out = append(out, clientTotal{key: key, name: format.StringOr(info.Name, ""), total: info.Total})
		}
	} else {
		totals := make(map[string]int)
		for _, h := range r.History {
			for key, n := range h.Data {
				totals[key] += n
			}
		}
		for key, n := range totals {
			out = append(out, clientTotal{key: key, total: n})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].total != out[j].total {
			return out[i].total > out[j].total
		}
		return out[i].key < out[j].key
	})
	return out
}

// unattributedKeys returns the per-slot bucket keys FTL did not declare as
// clients, sorted. The long-term endpoint keys its buckets by database row id
// while naming its clients by address, so the two cannot be reconciled here.
// Presenting the ids as client names would be an invented answer, and dropping
// them would hide the only per-slot numbers there are, so they are reported as
// what they are.
func unattributedKeys(r pihole.ClientHistoryResponse) []string {
	if len(r.Clients) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var keys []string
	for _, h := range r.History {
		for k := range h.Data {
			if _, declared := r.Clients[k]; declared || seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// label renders a client as "name (ip)" when FTL knows a hostname for it.
func (c clientTotal) label() string {
	if c.name == "" {
		return c.key
	}
	return fmt.Sprintf("%s (%s)", c.name, c.key)
}

func formatClientHistorySummary(r pihole.ClientHistoryResponse, detail string, csv bool) string {
	ranked := rankClients(r)
	if len(ranked) == 0 {
		return "No client history data available."
	}

	if detail == "minimal" {
		return fmt.Sprintf("**%d clients.**\n", len(ranked))
	}

	unattributed := unattributedKeys(r)

	if detail == "full" {
		return clientHistorySeries(r, ranked, unattributed)
	}

	if csv {
		headers := []string{"Client", "Name", "Queries"}
		rows := make([][]string, 0, len(ranked))
		for _, c := range ranked {
			rows = append(rows, []string{c.key, c.name, strconv.Itoa(c.total)})
		}
		return format.CSV(headers, rows)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%d clients:**\n", len(ranked))
	for _, c := range ranked {
		fmt.Fprintf(&b, "- %s — %s queries\n", c.label(), format.Number(c.total))
	}
	writeUnattributedNote(&b, unattributed)
	return b.String()
}

// writeUnattributedNote says so when Pi-hole sent per-slot buckets it did not
// map to any client it named, rather than leaving the totals above looking
// like the whole picture.
func writeUnattributedNote(b *strings.Builder, unattributed []string) {
	if len(unattributed) == 0 {
		return
	}
	fmt.Fprintf(b, "\n_Pi-hole also returned %d per-slot buckets it does not map to a client address (%s). Use detail=full to see their counts._\n",
		len(unattributed), strings.Join(unattributed, ", "))
}

// clientHistorySeries renders the per-slot activity as one row per time slot
// and one column per client, ordered the same way as the summary. Any bucket
// Pi-hole did not attribute to a client gets a column under its own key, so
// the numbers stay reachable without being relabelled as something they are
// not.
func clientHistorySeries(r pihole.ClientHistoryResponse, ranked []clientTotal, unattributed []string) string {
	if len(r.History) == 0 {
		return "No per-slot client history returned."
	}

	keys := make([]string, 0, len(ranked)+len(unattributed))
	headers := make([]string, 0, len(ranked)+len(unattributed)+1)
	headers = append(headers, "Time")
	for _, c := range ranked {
		keys = append(keys, c.key)
		headers = append(headers, c.label())
	}
	for _, k := range unattributed {
		keys = append(keys, k)
		headers = append(headers, "bucket "+k)
	}

	rows := make([][]string, 0, len(r.History))
	for _, h := range r.History {
		row := make([]string, 0, len(keys)+1)
		row = append(row, format.Timestamp(h.Timestamp))
		for _, k := range keys {
			row = append(row, strconv.Itoa(h.Data[k]))
		}
		rows = append(rows, row)
	}

	return format.CSV(headers, rows)
}
