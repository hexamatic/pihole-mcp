// Package tools registers all MCP tool definitions for the Pi-hole MCP server.
package tools

import (
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterAll registers every tool category on the MCP server.
// Each tool handler is wrapped with instance routing and OpenTelemetry tracing
// (the latter is a noop when OTel is not configured).
func RegisterAll(s *server.MCPServer, r *pihole.Registry) {
	RegisterPADD(s, r)
	RegisterDNS(s, r)
	RegisterStats(s, r)
	RegisterInfo(s, r)
	RegisterQueries(s, r)
	RegisterHistory(s, r)
	RegisterSearch(s, r)
	RegisterDomains(s, r)
	RegisterGroups(s, r)
	RegisterClients(s, r)
	RegisterLists(s, r)
	RegisterConfig(s, r)
	RegisterActions(s, r)
	RegisterNetwork(s, r)
	RegisterDHCP(s, r)
	RegisterLogs(s, r)
	RegisterTeleporter(s, r)
	RegisterAuth(s, r)
	RegisterSync(s, r)
}

// addTool registers a tool with instance-routing and tracing middleware. When
// more than one instance is configured it also advertises the optional
// "instance" argument on the tool's input schema and drops the per-tool output
// schema: a multi-instance tool may return either a single-instance result or
// an instance=all aggregate envelope, so a single fixed output schema would be
// misleading. Structured content is still emitted, just without an advertised
// schema. Single-instance setups keep their full output schemas.
func addTool(s *server.MCPServer, r *pihole.Registry, tool mcp.Tool, handler server.ToolHandlerFunc) {
	normaliseReadOnlyAnnotations(&tool)
	if r.Len() > 1 {
		addInstanceParam(&tool, r)
		tool.OutputSchema = mcp.ToolOutputSchema{}
		tool.RawOutputSchema = nil
	}
	s.AddTool(tool, withTracing(tool.Name, instanceAware(r, tool, handler)))
}

// normaliseReadOnlyAnnotations makes a read-only tool's hints internally
// consistent. mcp-go's NewTool defaults DestructiveHint and OpenWorldHint to
// true; for a tool that does not modify state and only queries the configured
// Pi-hole, both should be false. This is a no-op for tools that are not
// annotated read-only, so deliberate openWorld/destructive hints on write
// tools are preserved.
func normaliseReadOnlyAnnotations(tool *mcp.Tool) {
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		return
	}
	no := false
	tool.Annotations.DestructiveHint = &no
	tool.Annotations.OpenWorldHint = &no
}
