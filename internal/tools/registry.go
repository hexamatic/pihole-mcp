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
	RegisterLocalDNS(s, r)
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
	applyToolTitle(&tool)
	if r.Len() > 1 {
		addInstanceParam(&tool, r)
		widenOutputSchema(&tool)
	}
	recordTool(tool)
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
// The generated AggregateOutput schema runs to about 1,100 bytes, and
// widenOutputSchema copies it into every tool that has an output schema. That
// is the same paragraph of JSON repeated seven times in one tools/list. What a
// client needs from this branch is the shape (an envelope of per-instance
// records) and the discriminator that tells it apart from the single-instance
// branch, so the descriptions and the inner result shape are dropped and the
// two required keys are kept. AggregateOutput itself is unchanged: this trims
// what is advertised, not what is returned.
func aggregateOutputSchema() json.RawMessage {
	aggregateSchemaOnce.Do(func() {
		b, err := json.Marshal(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"total":  map[string]any{"type": "integer"},
						"ok":     map[string]any{"type": "integer"},
						"failed": map[string]any{"type": "integer"},
					},
					"required": []string{"total", "ok", "failed"},
				},
				"instances": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"instance": map[string]any{"type": "string"},
							"ok":       map[string]any{"type": "boolean"},
						},
						"required": []string{"instance", "ok"},
					},
				},
			},
			"required": []string{"summary", "instances"},
			// Closed on purpose. This branch is one half of a oneOf, so it has
			// to reject a payload carrying the single-instance fields as well
			// as its own; an open branch would match both and oneOf would then
			// reject a correct payload. The generated schema got this from
			// jsonschema-go's additionalProperties, and dropping it was what
			// TestWidenOutputSchema_BranchesAreDisjoint caught.
			"additionalProperties": false,
		})
		if err == nil {
			aggregateSchema = b
		}
	})
	return aggregateSchema
}

var (
	aggregateSchemaOnce sync.Once
	aggregateSchema     json.RawMessage
)

// additiveTools are the *_add tools that only ever create a new row or append
// a value to a config array. Nothing existing is overwritten or removed, so
// mcp-go's destructive-by-default is the wrong hint for these specifically —
// unlike the four *_update tools, which do overwrite an existing row and stay
// destructive by that same default.
var additiveTools = map[string]bool{
	"pihole_domains_add":      true,
	"pihole_groups_add":       true,
	"pihole_clients_add":      true,
	"pihole_lists_add":        true,
	"pihole_config_add_value": true,
	"pihole_local_dns_add":    true,
	"pihole_local_cname_add":  true,
}

// normaliseReadOnlyAnnotations makes a tool's destructive/open-world hints
// internally consistent with what it actually does. mcp-go's NewTool defaults
// both to true; for a tool that does not modify state and only queries the
// configured Pi-hole, both should be false, and for a tool that only ever
// adds, DestructiveHint alone should be false. This is a no-op for every
// other tool, so deliberate openWorld/destructive hints on write and delete
// tools are preserved.
// applyToolTitle promotes the title annotation to the tool's own Title field.
//
// Every tool here carries a human-readable title as an annotation, which is
// where the field lived before the 2025-11-25 revision moved it onto the tool
// itself. Clients written against the current spec read Tool.Title and fall
// back to the bare tool name, so leaving it unset showed "pihole_stats_summary"
// in a picker that could have shown "Query Statistics". Setting both keeps
// older clients working.
func applyToolTitle(tool *mcp.Tool) {
	if tool.Title == "" {
		tool.Title = tool.Annotations.Title
	}
}

func normaliseReadOnlyAnnotations(tool *mcp.Tool) {
	if IsReadOnly(*tool) {
		no := false
		tool.Annotations.DestructiveHint = &no
		tool.Annotations.OpenWorldHint = &no
		return
	}
	if additiveTools[tool.Name] {
		no := false
		tool.Annotations.DestructiveHint = &no
	}
}
