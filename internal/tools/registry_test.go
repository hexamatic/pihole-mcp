package tools

import (
	"encoding/json"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// compile compiles a raw JSON schema the same way mcp-go's output validator
// does, so a schema that passes here is one it will accept at runtime.
func compile(t *testing.T, raw json.RawMessage) *jsonschema.Schema {
	t.Helper()

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", doc); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	s, err := c.Compile("schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return s
}

// validates reports whether value satisfies the schema.
func validates(t *testing.T, s *jsonschema.Schema, value any) error {
	t.Helper()

	// Round-trip through JSON so the value is the plain any-tree the validator
	// expects, exactly as it would arrive over the wire.
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal value: %v", err)
	}
	return s.Validate(doc)
}

// multiInstanceTool builds a tool through addTool with two instances configured
// and hands back the registered definition.
func multiInstanceTool(t *testing.T, tool mcp.Tool) mcp.Tool {
	t.Helper()

	r := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "alpha", URL: "http://alpha.invalid", Password: "x"},
		{Name: "beta", URL: "http://beta.invalid", Password: "x"},
	})
	if r.Len() != 2 {
		t.Fatalf("registry has %d instances, want 2", r.Len())
	}

	normaliseReadOnlyAnnotations(&tool)
	addInstanceParam(&tool, r)
	widenOutputSchema(&tool)
	return tool
}

func TestWidenOutputSchema_AcceptsBothShapes(t *testing.T) {
	tool := multiInstanceTool(t, mcp.NewTool("pihole_stats_top_domains",
		mcp.WithOutputSchema[TopListOutput](),
	))

	if tool.RawOutputSchema == nil {
		t.Fatal("multi-instance tool advertises no output schema — configuring a second Pi-hole must not strip structured output")
	}
	if tool.OutputSchema.Type != "" {
		t.Fatal("both OutputSchema and RawOutputSchema are set; mcp-go rejects that combination")
	}

	schema := compile(t, tool.RawOutputSchema)

	// Branch 1: the tool was targeted at a single instance.
	single := TopListOutput{
		Items:   []TopItemOutput{{Name: "github.com", Count: 5}},
		Count:   1,
		Blocked: false,
	}
	if err := validates(t, schema, single); err != nil {
		t.Errorf("single-instance result rejected by the widened schema: %v", err)
	}

	// Branch 2: the tool was called with instance=all.
	aggregate := AggregateOutput{
		Summary: AggregateSummary{Total: 2, OK: 2, Failed: 0},
		Instances: []AggregateInstanceResult{
			{Instance: "alpha", OK: true, Data: single, Text: "..."},
			{Instance: "beta", OK: true, Data: single, Text: "..."},
		},
	}
	if err := validates(t, schema, aggregate); err != nil {
		t.Errorf("instance=all aggregate rejected by the widened schema: %v", err)
	}
}

func TestWidenOutputSchema_BranchesAreDisjoint(t *testing.T) {
	// oneOf requires exactly one branch to match. If a payload satisfied both,
	// validation would fail even though the payload is correct — so the two
	// shapes must not overlap.
	tool := multiInstanceTool(t, mcp.NewTool("pihole_stats_top_domains",
		mcp.WithOutputSchema[TopListOutput](),
	))
	schema := compile(t, tool.RawOutputSchema)

	// A payload carrying both sets of fields matches both branches, so oneOf
	// must reject it. This proves the branches are genuinely exclusive rather
	// than accidentally passing because one is permissive.
	both := map[string]any{
		"items":   []any{},
		"count":   0,
		"blocked": false,
		"summary": map[string]any{"total": 1, "ok": 1, "failed": 0},
		"instances": []any{
			map[string]any{"instance": "alpha", "ok": true},
		},
	}
	if err := validates(t, schema, both); err == nil {
		t.Error("a payload matching both branches was accepted; oneOf is not discriminating between them")
	}
}

func TestWidenOutputSchema_LeavesSchemalessToolsAlone(t *testing.T) {
	tool := multiInstanceTool(t, mcp.NewTool("pihole_actions_restart_dns"))

	if tool.RawOutputSchema != nil {
		t.Errorf("a tool with no output schema should not gain one, got: %s", tool.RawOutputSchema)
	}
}
