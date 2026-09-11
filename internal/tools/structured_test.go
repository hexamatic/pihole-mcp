package tools

import (
	"context"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// callToolResult invokes a handler and returns the full result so tests can
// assert on structured content as well as text.
func callToolResult(t *testing.T, handlerFn func(*pihole.Registry) server.ToolHandlerFunc, c *pihole.Client, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handlerFn(pihole.SingleRegistry(c))(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %v", result.Content)
	}
	return result
}

// textOf returns the text content of a result, for tests that assert on both
// the rendered text and the structured payload.
func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("first content block is %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

func TestStructuredOutput_PADD(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{"/padd": loadFixture(t, "padd")}))
	res := callToolResult(t, paddHandler, c, nil)
	out, ok := res.StructuredContent.(PADDOutput)
	if !ok {
		t.Fatalf("expected PADDOutput structured content, got %T", res.StructuredContent)
	}
	if out.TotalQueries != 48213 || out.Blocking != "enabled" {
		t.Errorf("unexpected structured values: %+v", out)
	}
}

func TestStructuredOutput_TopDomains(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/top_domains": loadFixture(t, "stats_top_domains"),
	}))
	res := callToolResult(t, statsTopDomainsHandler, c, nil)
	out, ok := res.StructuredContent.(TopListOutput)
	if !ok {
		t.Fatalf("expected TopListOutput structured content, got %T", res.StructuredContent)
	}
	if out.Count != len(out.Items) || out.Count == 0 {
		t.Errorf("expected non-empty consistent item count, got %+v", out)
	}
}

func TestStructuredOutput_TopDomainsCSV(t *testing.T) {
	// Structured content must be present even when the CSV text format is used.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/top_domains": loadFixture(t, "stats_top_domains"),
	}))
	res := callToolResult(t, statsTopDomainsHandler, c, map[string]any{"format": "csv"})
	if _, ok := res.StructuredContent.(TopListOutput); !ok {
		t.Fatalf("expected TopListOutput structured content with csv format, got %T", res.StructuredContent)
	}
}

func TestStructuredOutput_InfoSystem(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/system": loadFixture(t, "info_system"),
	}))
	res := callToolResult(t, infoSystemHandler, c, nil)
	out, ok := res.StructuredContent.(InfoSystemOutput)
	if !ok {
		t.Fatalf("expected InfoSystemOutput structured content, got %T", res.StructuredContent)
	}
	if out.CPUCores == 0 && out.MemoryTotal == 0 {
		t.Errorf("expected populated system fields, got %+v", out)
	}
}
