// Package tools registers all MCP tool definitions for the Pi-hole MCP server.
package tools

import (
	"encoding/json"
	"sync"

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
// "instance" argument on the tool's input schema and widens the output schema
// to cover both shapes the tool can now return.
func addTool(s *server.MCPServer, r *pihole.Registry, tool mcp.Tool, handler server.ToolHandlerFunc) {
	normaliseReadOnlyAnnotations(&tool)
	if r.Len() > 1 {
		addInstanceParam(&tool, r)
		widenOutputSchema(&tool)
	}
	s.AddTool(tool, withTracing(tool.Name, instanceAware(r, tool, handler)))
}

// widenOutputSchema rewrites a tool's output schema as "either the single
// instance result, or the instance=all aggregate envelope".
//
// With more than one Pi-hole configured, a tool returns its own result shape
// when targeted at one instance and an AggregateOutput when called with
// instance=all. Declaring only the former would be a lie half the time. This
// previously resolved the contradiction by discarding the output schema
// altogether, which meant that configuring a second Pi-hole silently stripped
// structured output from every tool that had it — the flagship feature
// degrading the very thing it should showcase. A oneOf states both shapes
// honestly, and they are disjoint (each requires fields the other lacks), so
// exactly one branch matches any given payload.
//
// Tools with no output schema to begin with are left alone.
func widenOutputSchema(tool *mcp.Tool) {
	single := tool.RawOutputSchema
	if single == nil {
		if tool.OutputSchema.Type == "" {
			return
		}
		b, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			return
		}
		single = b
	}

	aggregate := aggregateOutputSchema()
	if aggregate == nil {
		return
	}

	composed, err := json.Marshal(map[string]any{
		"type":  "object",
		"oneOf": []json.RawMessage{single, aggregate},
	})
	if err != nil {
		return
	}

	// mcp-go rejects a tool that carries both; the raw schema is the one that
	// can express oneOf.
	tool.OutputSchema = mcp.ToolOutputSchema{}
	tool.RawOutputSchema = composed
}

// aggregateOutputSchema returns the JSON Schema for AggregateOutput, generated
// once from the Go type by the same code path as every other output schema so
// it cannot drift from the struct.
func aggregateOutputSchema() json.RawMessage {
	aggregateSchemaOnce.Do(func() {
		probe := mcp.NewTool("aggregate", mcp.WithOutputSchema[AggregateOutput]())
		if probe.OutputSchema.Type == "" {
			return
		}
		if b, err := json.Marshal(probe.OutputSchema); err == nil {
			aggregateSchema = b
		}
	})
	return aggregateSchema
}

var (
	aggregateSchemaOnce sync.Once
	aggregateSchema     json.RawMessage
)

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
