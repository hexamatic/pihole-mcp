// Package server constructs and configures the MCP server.
package server

import (
	"runtime/debug"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/config"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/prompts"
	"github.com/hexamatic/pihole-mcp/internal/resources"
	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Version is set at build time via ldflags.
var Version = "dev"

// A binary installed with `go install ...@latest` carries no ldflags, so
// Version would read "dev" for everyone who installs that way. The module
// version the toolchain stamped into the build info is the real answer, so
// prefer it whenever the ldflag was not supplied. Builds from a working tree
// report "(devel)", which is no more useful than "dev", so those keep "dev".
func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = nil
	}
	Version = resolveVersion(Version, info)
}

// resolveVersion picks between the ldflag value and the module version the
// toolchain recorded. The ldflag wins whenever goreleaser supplied one;
// "(devel)", which is what a build from a working tree reports, is no more
// informative than "dev" and is discarded.
func resolveVersion(ldflag string, info *debug.BuildInfo) string {
	if ldflag != "dev" {
		return ldflag
	}
	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ldflag
	}
	// goreleaser passes the version without a leading "v"; match it so both
	// install routes print the same string.
	return strings.TrimPrefix(info.Main.Version, "v")
}

// icons converts the shared icon set into the protocol's representation.
func icons() []mcp.Icon {
	out := make([]mcp.Icon, 0, len(config.ServerIcons))
	for _, i := range config.ServerIcons {
		out = append(out, mcp.Icon{Src: i.Src, MIMEType: i.MIMEType, Sizes: i.Sizes})
	}
	return out
}

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
		// Without this, a panic in a resource handler escapes HandleMessage on
		// the stdio read loop and takes the whole process down with it. Only
		// the tool path was covered before.
		server.WithResourceRecovery(),
		// The output schemas are generated from the Go structs the handlers
		// return, so they are accurate by construction and a mismatch is a real
		// bug worth surfacing. Input validation is deliberately not enabled:
		// mcp-go coerces a numeric string such as {"count":"5"} through
		// strconv.ParseFloat today, and LLM clients send exactly that, so
		// turning it on would break working calls.
		server.WithOutputSchemaValidation(),
		server.WithTitle(config.ServerTitle),
		server.WithDescription(config.ServerDescription),
		server.WithWebsiteURL(config.ServerWebsiteURL),
		server.WithIcons(icons()...),
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
