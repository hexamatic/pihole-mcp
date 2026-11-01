// Package prompts registers MCP prompts for the Pi-hole MCP server.
package prompts

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterAll registers all MCP prompts.
func RegisterAll(s *server.MCPServer) {
	s.AddPrompt(mcp.NewPrompt("diagnose_slow_dns",
		mcp.WithPromptDescription("Diagnose DNS performance issues by analysing upstream response times, query patterns, and system resources."),
	), diagnoseSlowDNSHandler)

	s.AddPrompt(mcp.NewPrompt("investigate_domain",
		mcp.WithPromptDescription("Investigate why a domain is blocked or allowed by searching across all lists and checking recent query logs."),
		mcp.WithArgument("domain",
			mcp.ArgumentDescription("The domain to investigate"),
			mcp.RequiredArgument(),
		),
	), investigateDomainHandler)

	s.AddPrompt(mcp.NewPrompt("review_top_blocked",
		mcp.WithPromptDescription("Review the top blocked domains and identify potential false positives that should be allowlisted."),
		mcp.WithArgument("count",
			mcp.ArgumentDescription("Number of top blocked domains to review (default 20)"),
		),
	), reviewTopBlockedHandler)

	s.AddPrompt(mcp.NewPrompt("audit_network",
		mcp.WithPromptDescription("Audit network devices, configured clients, and DHCP leases to identify unknown or suspicious devices."),
	), auditNetworkHandler)

	s.AddPrompt(mcp.NewPrompt("optimise_blocklists",
		mcp.WithPromptDescription("Analyse configured blocklists and suggest consolidation or removal of redundant lists."),
	), optimiseBlocklistsHandler)

	s.AddPrompt(mcp.NewPrompt("daily_report",
		mcp.WithPromptDescription("Generate a comprehensive daily Pi-hole summary covering queries, blocking, clients, upstreams, and system health."),
	), dailyReportHandler)

	s.AddPrompt(mcp.NewPrompt("security_audit",
		mcp.WithPromptDescription("Review active API sessions, authentication configuration, and diagnostic messages to detect potential unauthorised access."),
	), securityAuditHandler)

	s.AddPrompt(mcp.NewPrompt("weekly_trends",
		mcp.WithPromptDescription("Compare this week's DNS statistics to last week using long-term database queries to identify trends."),
		mcp.WithArgument("weeks_back",
			mcp.ArgumentDescription("Number of weeks to compare against (default 1)"),
		),
	), weeklyTrendsHandler)

	s.AddPrompt(mcp.NewPrompt("upstream_health",
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
					"3. Based on the results, explain:\n"+
					"   - Is this domain currently blocked? If so, by which mechanism (gravity, exact deny, regex)?\n"+
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
					"- Recommend group assignments for unconfigured clients\n"+
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
	weeksBack := req.Params.Arguments["weeks_back"]
	if weeksBack == "" {
		weeksBack = "1"
	}

	return &mcp.GetPromptResult{
		Description: "Weekly DNS trend analysis",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
				fmt.Sprintf("Analyse DNS trends by comparing this week to %s week(s) ago.\n\n"+
					"Follow these steps:\n"+
					"1. Use pihole_stats_database with this week's date range to get current totals\n"+
					"2. Use pihole_stats_database with last week's equivalent date range for comparison\n"+
					"3. Use pihole_stats_database_top_domains with blocked=true for both periods to compare top blocked domains\n"+
					"4. Use pihole_stats_database_top_clients for both periods to compare client activity\n"+
					"5. Use pihole_stats_database_upstreams for both periods to compare how many queries each upstream answered\n\n"+
					"Present your findings as a trend report:\n"+
					"- Query volume change (increase/decrease percentage)\n"+
					"- Blocking rate change\n"+
					"- New domains appearing in top blocked that weren't there last week\n"+
					"- Client activity changes (new high-volume clients, departures)\n"+
					"- Changes in how queries were spread across upstream servers\n"+
					"- Overall assessment and recommendations",
					weeksBack),
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
