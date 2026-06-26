// Package server constructs and configures the MCP server.
package server

import (
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/prompts"
	"github.com/hexamatic/pihole-mcp/internal/resources"
	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/server"
)

// Version is set at build time via ldflags.
var Version = "dev"

// New creates a configured MCP server with all Pi-hole tools, resources,
// and prompts registered against the supplied instance registry.
func New(registry *pihole.Registry) *server.MCPServer {
	s := server.NewMCPServer(
		"pihole-mcp",
		Version,
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(false, false),
		server.WithPromptCapabilities(false),
		server.WithLogging(),
		server.WithCompletions(),
		server.WithPromptCompletionProvider(prompts.NewCompletionProvider(registry)),
		server.WithRecovery(),
		server.WithInstructions(
			"Pi-hole v6 DNS management server. "+
				"Start with pihole_padd for a one-call dashboard (queries, blocking, top domain/client, cache, versions, host health); use pihole_stats_summary when you need query detail. "+
				"Use pihole_search_domains before pihole_domains_add to check for duplicates. "+
				"After pihole_lists_add or pihole_lists_delete, run pihole_action_gravity_update to apply changes. "+
				"Use pihole_queries_suggestions to discover valid filter values for pihole_queries_search. "+
				"For time-range queries, use pihole_stats_database_* tools with from/until timestamps. "+
				"pihole_info_ftl provides dnsmasq-internal metrics not available in pihole_info_system. "+
				"When adding multiple upstream DNS servers, use pihole_config_add_value with restart=false for all but the last change. "+
				"If a pihole_config_set call is rejected as read-only, run pihole_config_properties to confirm which keys are locked by pihole.toml or env vars. "+
				"Recent Pi-hole FTL keys settable via pihole_config_set include resolver.macNames (FTL v6.6, MAC-based hostname resolution), database.forceDisk (v6.5, lower RAM use), and dns.cache.rrtype (v6.5, per-RR-type caching). "+
				"Tools accept optional 'detail' (minimal/normal/full) and 'format' (text/csv) parameters.",
		),
	)

	tools.RegisterAll(s, registry)
	resources.RegisterAll(s, registry)
	prompts.RegisterAll(s)

	return s
}
