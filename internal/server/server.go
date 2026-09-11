// Package server constructs and configures the MCP server.
package server

import (
	"context"
	"runtime/debug"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/config"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/prompts"
	"github.com/hexamatic/pihole-mcp/internal/resources"
	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/hexamatic/pihole-mcp/internal/toolsets"
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

// Option configures the tool surface a server exposes.
//
// Scoping is passed in rather than read from the environment here on purpose.
// cmd/toolsdoc builds a real server through this same constructor to harvest
// the tool catalogue, so a New that consulted the environment would make
// docs/TOOLS.md depend on the shell it was generated in, and the CI drift check
// would pass or fail according to whoever ran it last.
type Option func(*options)

type options struct {
	readOnly bool
	toolsets []string
}

// WithReadOnly restricts the server to tools annotated read-only.
func WithReadOnly(enabled bool) Option {
	return func(o *options) { o.readOnly = enabled }
}

// WithToolsets restricts the server to the named published toolsets. An empty
// selection, or one containing "all", places no restriction.
func WithToolsets(names []string) Option {
	return func(o *options) { o.toolsets = names }
}

// readOnlyFilter drops every tool that can change Pi-hole.
func readOnlyFilter(_ context.Context, candidates []mcp.Tool) []mcp.Tool {
	kept := make([]mcp.Tool, 0, len(candidates))
	for _, t := range candidates {
		if tools.IsReadOnly(t) {
			kept = append(kept, t)
		}
	}
	return kept
}

// toolsetFilter drops every tool outside the selected toolsets. A tool whose
// family token belongs to no toolset is dropped rather than passed through:
// TestEveryRegisteredToolHasAToolset makes that unreachable, and failing closed
// is the right way for an access control filter to handle a case it does not
// recognise.
func toolsetFilter(selected map[string]bool) server.ToolFilterFunc {
	return func(_ context.Context, candidates []mcp.Tool) []mcp.Tool {
		kept := make([]mcp.Tool, 0, len(candidates))
		for _, t := range candidates {
			if name, ok := toolsets.Of(t.Name); ok && selected[name] {
				kept = append(kept, t)
			}
		}
		return kept
	}
}

// filters builds the tool filter chain for a configuration.
//
// Two filters rather than one combined predicate. mcp-go applies them in
// sequence, each consuming the previous result, so they compose as an
// intersection without any hand-written boolean logic, and neither is installed
// at all in the default configuration. Both are enforced on tools/call as well
// as tools/list, which is what makes read-only mode an access control boundary
// rather than a visibility hint.
func (o options) filters() []server.ToolFilterFunc {
	var out []server.ToolFilterFunc
	if o.readOnly {
		out = append(out, readOnlyFilter)
	}
	if selected := toolsets.Resolve(o.toolsets); selected != nil {
		out = append(out, toolsetFilter(selected))
	}
	return out
}

// ExposedTools reports which of a server's registered tools the supplied
// scoping actually offers a client.
//
// It exists because srv.ListTools returns the raw registration map and never
// consults a filter, so it always answers with the full surface. Callers that
// want the client's view have to run the chain themselves, which is what this
// does, over the same closures New installs.
func ExposedTools(srv *server.MCPServer, opts ...Option) []mcp.Tool {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	registered := srv.ListTools()
	candidates := make([]mcp.Tool, 0, len(registered))
	for _, st := range registered {
		candidates = append(candidates, st.Tool)
	}
	for _, f := range o.filters() {
		candidates = f(context.Background(), candidates)
	}
	return candidates
}

// New creates a configured MCP server with all Pi-hole tools, resources,
// and prompts registered against the supplied instance registry.
func New(registry *pihole.Registry, opts ...Option) *server.MCPServer {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	serverOpts := []server.ServerOption{
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
		server.WithInstructions(instructions(o.readOnly)),
	}

	for _, f := range o.filters() {
		serverOpts = append(serverOpts, server.WithToolFilter(f))
	}

	s := server.NewMCPServer("pihole-mcp", Version, serverOpts...)

	tools.RegisterAll(s, registry)
	resources.RegisterAll(s, registry)
	prompts.RegisterAll(s)

	return s
}
