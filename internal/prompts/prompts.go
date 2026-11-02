// Package prompts registers MCP prompts for the Pi-hole MCP server.
package prompts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// promptNeeds are the tools a prompt's steps use.
type promptNeeds struct {
	// core are the tools the prompt cannot do without. It is offered only
	// when every one of them is.
	core []string
	// extra are tools for steps the prompt can do without. When one is not
	// offered, the prompt names it and says to skip the steps that use it.
	extra []string
}

// promptTools are the tools each prompt directs the reader to use. A
// deployment narrowed with PIHOLE_TOOLSETS used to offer every prompt, whose
// steps then named tools it did not have; then only prompts whose every tool
// it had, which took all nine from a curation setup such as
// dashboard,domains,lists over one statistics step. A prompt now needs only
// its core tools. TestPromptToolsMatchTheirText keeps this in step with the
// prompts.
var promptTools = map[string]promptNeeds{
	"diagnose_slow_dns":   {[]string{"pihole_stats_upstreams"}, []string{"pihole_info_system", "pihole_queries_search", "pihole_stats_summary"}},
	"investigate_domain":  {[]string{"pihole_search_domains"}, []string{"pihole_local_cname_list", "pihole_local_dns_list", "pihole_queries_search"}},
	"review_top_blocked":  {[]string{"pihole_stats_top_domains"}, []string{"pihole_domains_list", "pihole_search_domains"}},
	"audit_network":       {[]string{"pihole_network_devices"}, []string{"pihole_clients_list", "pihole_clients_suggestions", "pihole_dhcp_leases"}},
	"optimise_blocklists": {[]string{"pihole_lists_list"}, []string{"pihole_stats_summary"}},
	"daily_report":        {[]string{"pihole_stats_summary"}, []string{"pihole_info_system", "pihole_stats_top_clients", "pihole_stats_top_domains", "pihole_stats_upstreams"}},
	"security_audit":      {[]string{"pihole_auth_sessions"}, []string{"pihole_config_get_value", "pihole_info_ftl", "pihole_info_messages"}},
	"weekly_trends":       {[]string{"pihole_stats_database"}, []string{"pihole_stats_database_top_clients", "pihole_stats_database_top_domains", "pihole_stats_database_upstreams"}},
	"upstream_health":     {[]string{"pihole_stats_upstreams"}, []string{"pihole_info_ftl", "pihole_info_metrics", "pihole_stats_database_upstreams", "pihole_stats_query_types"}},
}

// RegisterAll registers the MCP prompts whose core tools offered reports as
// available. A nil offered registers them all as written.
func RegisterAll(s *server.MCPServer, offered func(toolName string) bool) {
	add := func(p mcp.Prompt, h server.PromptHandlerFunc) {
		if offered != nil {
			needs := promptTools[p.Name]
			for _, t := range needs.core {
				if !offered(t) {
					return
				}
			}
			var missing []string
			for _, t := range needs.extra {
				if !offered(t) {
					missing = append(missing, t)
				}
			}
			if len(missing) > 0 {
				h = skipping(h, missing)
			}
		}
		s.AddPrompt(p, h)
	}
	registerPrompts(add)
}

// skipping wraps a prompt handler to end its text by naming the tools this
// server does not offer, so the steps that use them are skipped rather than
// attempted.
func skipping(h server.PromptHandlerFunc, missing []string) server.PromptHandlerFunc {
	them := "it"
	if len(missing) > 1 {
		them = "them"
	}
	note := fmt.Sprintf("\n\nThis server does not offer %s, so skip the steps that use %s and say what was left out.",
		strings.Join(missing, ", "), them)
	return func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		res, err := h(ctx, req)
		if err != nil || res == nil || len(res.Messages) == 0 {
			return res, err
		}
		last := &res.Messages[len(res.Messages)-1]
		if tc, ok := last.Content.(mcp.TextContent); ok {
			tc.Text += note
			last.Content = tc
		}
		return res, nil
	}
}

func registerPrompts(add func(mcp.Prompt, server.PromptHandlerFunc)) {
	add(mcp.NewPrompt("diagnose_slow_dns",
		mcp.WithPromptDescription("Diagnose DNS performance issues by analysing upstream response times, query patterns, and system resources."),
	), diagnoseSlowDNSHandler)

	add(mcp.NewPrompt("investigate_domain",
		mcp.WithPromptDescription("Investigate why a domain is blocked or allowed by searching across all lists and checking recent query logs."),
		mcp.WithArgument("domain",
			mcp.ArgumentDescription("The domain to investigate"),
			mcp.RequiredArgument(),
		),
	), investigateDomainHandler)

	add(mcp.NewPrompt("review_top_blocked",
		mcp.WithPromptDescription("Review the top blocked domains and identify potential false positives that should be allowlisted."),
		mcp.WithArgument("count",
			mcp.ArgumentDescription("Number of top blocked domains to review (default 20)"),
		),
	), reviewTopBlockedHandler)

	add(mcp.NewPrompt("audit_network",
		mcp.WithPromptDescription("Audit network devices, configured clients, and DHCP leases to identify unknown or suspicious devices."),
	), auditNetworkHandler)

	add(mcp.NewPrompt("optimise_blocklists",
		mcp.WithPromptDescription("Analyse configured blocklists and suggest consolidation or removal of redundant lists."),
	), optimiseBlocklistsHandler)

	add(mcp.NewPrompt("daily_report",
		mcp.WithPromptDescription("Generate a comprehensive daily Pi-hole summary covering queries, blocking, clients, upstreams, and system health."),
	), dailyReportHandler)

	add(mcp.NewPrompt("security_audit",
		mcp.WithPromptDescription("Review active API sessions, authentication configuration, and diagnostic messages to detect potential unauthorised access."),
	), securityAuditHandler)

	add(mcp.NewPrompt("weekly_trends",
		mcp.WithPromptDescription("Compare this week's DNS statistics to last week using long-term database queries to identify trends."),
		mcp.WithArgument("weeks_back",
			mcp.ArgumentDescription("Number of weeks to compare against (default 1)"),
		),
	), weeklyTrendsHandler)

	add(mcp.NewPrompt("upstream_health",
		mcp.WithPromptDescription("Deep analysis of DNS resolver performance, cache efficiency, and upstream response times with historical comparison."),
	), upstreamHealthHandler)
}

func diagnoseSlowDNSHandler(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Diagnose DNS performance issues",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				"Diagnose DNS performance issues on this Pi-hole instance.\n\n"+
					"Follow these steps:\n"+
					"1. Use pihole_stats_summary to get current query metrics and blocking rate\n"+
					"2. Use pihole_stats_upstreams to check upstream DNS server response times and variance\n"+
					"3. Use pihole_queries_search to find recent queries with slow replies or failures\n"+
					"4. Use pihole_info_system to check system resource usage (CPU, memory, load)\n\n"+
					"Based on the data, identify performance bottlenecks and recommend specific actions to improve DNS resolution speed.",
			)),
		},
	}, nil
}

func investigateDomainHandler(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	domain := req.Params.Arguments["domain"]
	if domain == "" {
		domain = "<domain>"
	}

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Investigate domain: %s", domain),
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				fmt.Sprintf("Investigate the domain **%s** on this Pi-hole instance.\n\n"+
					"Follow these steps:\n"+
					"1. Use pihole_search_domains with domain='%s' to check if it appears in any allow/deny lists or gravity blocklists\n"+
					"2. Use pihole_queries_search with domain='%s' to find recent query history for this domain\n"+
					"3. Use pihole_local_dns_list and pihole_local_cname_list to see whether this Pi-hole answers for the domain itself\n"+
					"4. Based on the results, explain:\n"+
					"   - Is this domain currently blocked, or answered by a local record? If blocked, by which mechanism (gravity, exact deny, regex)?\n"+
					"   - What list is responsible for the block?\n"+
					"   - How frequently is it queried and from which clients?\n"+
					"   - Recommend whether to allowlist, keep blocked, or take other action.",
					domain, domain, domain),
			)),
		},
	}, nil
}

func reviewTopBlockedHandler(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	count := req.Params.Arguments["count"]
	if count == "" {
		count = "20"
	}

	return &mcp.GetPromptResult{
		Description: "Review top blocked domains for false positives",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				fmt.Sprintf("Review the top %s blocked domains on this Pi-hole for potential false positives.\n\n"+
					"Follow these steps:\n"+
					"1. Use pihole_stats_top_domains with blocked=true and count=%s to get the most-blocked domains\n"+
					"2. Use pihole_domains_list with type='allow' to see the current allowlist\n"+
					"3. For each blocked domain, assess whether it is:\n"+
					"   - **Correct block:** Known advertising, tracking, or malware domain\n"+
					"   - **Possible false positive:** Legitimate service that may be needed (CDNs, APIs, update servers)\n"+
					"   - **Needs investigation:** Ambiguous — recommend using pihole_search_domains to check further\n\n"+
					"Present your findings as a categorised list with recommendations.",
					count, count),
			)),
		},
	}, nil
}

func auditNetworkHandler(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Audit network devices and clients",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				"Perform a network audit on this Pi-hole instance.\n\n"+
					"Follow these steps:\n"+
					"1. Use pihole_network_devices to list all devices seen on the network\n"+
					"2. Use pihole_clients_list to see configured clients with group assignments\n"+
					"3. Use pihole_clients_suggestions to find unconfigured devices\n"+
					"4. Use pihole_dhcp_leases to check active DHCP leases\n\n"+
					"Based on the data:\n"+
					"- Identify any devices that are not configured as Pi-hole clients\n"+
					"- Flag devices with unknown or suspicious MAC vendors\n"+
					"- Highlight devices with unusually high query counts\n"+
					"- Recommend group assignments for unconfigured clients (the clients add and update tools take a groups parameter)\n"+
					"- Note any DHCP leases that don't match known devices.",
			)),
		},
	}, nil
}

func optimiseBlocklistsHandler(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Optimise blocklists",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				"Analyse the Pi-hole blocklists and suggest optimisations.\n\n"+
					"Follow these steps:\n"+
					"1. Use pihole_lists_list to get all configured blocklists with their domain counts and status\n"+
					"2. Use pihole_stats_summary to see the total gravity count and blocking rate\n\n"+
					"Based on the data:\n"+
					"- Identify lists that are disabled or have failed to update\n"+
					"- Flag lists with very few domains (may not be worth the update overhead)\n"+
					"- Identify potentially overlapping lists (similar names or sources)\n"+
					"- Recommend whether to consolidate, remove, or add lists\n"+
					"- Suggest well-known quality blocklists that might be missing.",
			)),
		},
	}, nil
}

func dailyReportHandler(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Daily Pi-hole report",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				"Generate a comprehensive daily report for this Pi-hole instance.\n\n"+
					"Gather the following data:\n"+
					"1. Use pihole_stats_summary for overall query statistics and blocking rate\n"+
					"2. Use pihole_stats_top_domains with count=10 for top permitted domains\n"+
					"3. Use pihole_stats_top_domains with blocked=true and count=10 for top blocked domains\n"+
					"4. Use pihole_stats_top_clients with count=10 for most active clients\n"+
					"5. Use pihole_stats_upstreams for upstream DNS performance\n"+
					"6. Use pihole_info_system for system health (CPU, memory, load, temperature)\n\n"+
					"Format the report with clear sections, highlighting:\n"+
					"- Key metrics and any notable changes\n"+
					"- Upstream DNS health and response times\n"+
					"- System resource usage and any concerns\n"+
					"- Recommendations for action if any issues are found.",
			)),
		},
	}, nil
}

func securityAuditHandler(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Security audit",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				"Perform a security audit on this Pi-hole instance.\n\n"+
					"Follow these steps:\n"+
					"1. Use pihole_auth_sessions to list all active API sessions\n"+
					"2. Use pihole_config_get_value with element='webserver.api' to check authentication settings\n"+
					"3. Use pihole_info_messages to look for security-related warnings or failed authentication attempts\n"+
					"4. Use pihole_info_ftl to check the FTL privacy level setting\n\n"+
					"Based on the data:\n"+
					"- Identify any unexpected sessions (unknown IPs, unusual user agents)\n"+
					"- Verify authentication is properly configured\n"+
					"- Check if the privacy level is appropriate\n"+
					"- Flag any security-related diagnostic messages\n"+
					"- Recommend actions to improve security posture",
			)),
		},
	}, nil
}

func weeklyTrendsHandler(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	weeksBack := 1
	if v := req.Params.Arguments["weeks_back"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 52 {
			return nil, fmt.Errorf("weeks_back must be a whole number of weeks from 1 to 52 (got %q)", v)
		}
		weeksBack = n
	}

	// The periods are worked out here and given as timestamps. Left to the
	// reader, they were computed from a date the model assumed, and a wrong
	// guess compared the wrong weeks without anything showing it.
	const week = 7 * 24 * time.Hour
	now := time.Now()
	thisFrom, thisUntil := now.Add(-week).Unix(), now.Unix()
	thenUntil := now.Add(-time.Duration(weeksBack) * week)
	thenFrom := thenUntil.Add(-week).Unix()

	return &mcp.GetPromptResult{
		Description: "Weekly DNS trend analysis",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				fmt.Sprintf("Analyse DNS trends by comparing the last seven days with the same seven days %d week(s) earlier.\n\n"+
					"The periods, as Unix timestamps:\n"+
					"- This week: from=%d until=%d\n"+
					"- Comparison week: from=%d until=%d\n\n"+
					"Follow these steps, once for each period:\n"+
					"1. Use pihole_stats_database with the period's from and until to get its totals\n"+
					"2. Use pihole_stats_database_top_domains with blocked=true to compare top blocked domains\n"+
					"3. Use pihole_stats_database_top_clients to compare client activity\n"+
					"4. Use pihole_stats_database_upstreams to compare how many queries each upstream answered\n\n"+
					"Present your findings as a trend report:\n"+
					"- Query volume change (increase/decrease percentage)\n"+
					"- Blocking rate change\n"+
					"- New domains appearing in top blocked that weren't there in the comparison week\n"+
					"- Client activity changes (new high-volume clients, departures)\n"+
					"- Changes in how queries were spread across upstream servers\n"+
					"- Overall assessment and recommendations",
					weeksBack, thisFrom, thisUntil, thenFrom, thenUntil.Unix()),
			)),
		},
	}, nil
}

func upstreamHealthHandler(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Upstream DNS health analysis",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				"Perform a deep analysis of DNS resolver health on this Pi-hole instance.\n\n"+
					"Follow these steps:\n"+
					"1. Use pihole_stats_upstreams to get current upstream DNS server performance metrics\n"+
					"2. Use pihole_info_ftl to check FTL engine status, cache statistics, and DNSSEC configuration\n"+
					"3. Use pihole_info_metrics for detailed DNS operational counters\n"+
					"4. Use pihole_stats_database_upstreams with a 7-day range to see how query volume per upstream changed (the long-term database keeps no response times)\n"+
					"5. Use pihole_stats_query_types to understand the query type distribution\n\n"+
					"Based on the data, analyse:\n"+
					"- Individual upstream server health (response times, variance, reliability)\n"+
					"- Cache hit ratio and efficiency\n"+
					"- DNSSEC validation status\n"+
					"- Query type distribution anomalies\n"+
					"- Historical trends in query volume per upstream\n"+
					"- Recommend specific actions: replace slow upstreams, adjust cache settings, or add redundant resolvers",
			)),
		},
	}, nil
}
