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
	eachFixture(t, "padd", func(t *testing.T, _ string, fixture any) {
		c := newTestClient(t, piholeHandler(map[string]any{"/padd": fixture}))
		res := callToolResult(t, paddHandler, c, nil)
		out, ok := res.StructuredContent.(PADDOutput)
		if !ok {
			t.Fatalf("expected PADDOutput structured content, got %T", res.StructuredContent)
		}
		want := PADDOutput{
			Blocking:       fixtureString(t, fixture, "blocking"),
			TotalQueries:   fixtureInt(t, fixture, "queries.total"),
			BlockedQueries: fixtureInt(t, fixture, "queries.blocked"),
			ActiveClients:  fixtureInt(t, fixture, "active_clients"),
			GravitySize:    fixtureInt(t, fixture, "gravity_size"),
			TopDomain:      fixtureString(t, fixture, "top_domain"),
			CacheSize:      fixtureInt(t, fixture, "cache.size"),
			FTLVersion:     fixtureString(t, fixture, "version.ftl.local.version"),
			NodeName:       fixtureString(t, fixture, "node_name"),
		}
		got := PADDOutput{
			Blocking: out.Blocking, TotalQueries: out.TotalQueries, BlockedQueries: out.BlockedQueries,
			ActiveClients: out.ActiveClients, GravitySize: out.GravitySize, TopDomain: out.TopDomain,
			CacheSize: out.CacheSize, FTLVersion: out.FTLVersion, NodeName: out.NodeName,
		}
		if got != want {
			t.Errorf("structured values = %+v, want %+v", got, want)
		}
	})
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
	eachFixture(t, "info_system", func(t *testing.T, version string, sys any) {
		c := newTestClient(t, piholeHandler(systemRoutes(t, version)))
		res := callToolResult(t, infoSystemHandler, c, nil)
		out, ok := res.StructuredContent.(InfoSystemOutput)
		if !ok {
			t.Fatalf("expected InfoSystemOutput structured content, got %T", res.StructuredContent)
		}
		want := InfoSystemOutput{
			Hostname:    fixtureString(t, loadVersionFixture(t, version, "info_host"), "host.uname.nodename"),
			Uptime:      fixtureInt(t, sys, "system.uptime"),
			LoadAverage: fixtureValue(t, sys, "system.cpu.load.raw").([]any)[0].(float64),
			CPUCores:    fixtureInt(t, sys, "system.cpu.nprocs"),
			CPUPercent:  fixtureValue(t, sys, "system.cpu.%cpu").(float64),
			MemoryUsed:  fixtureValue(t, sys, "system.memory.ram.used").(float64),
			MemoryTotal: fixtureValue(t, sys, "system.memory.ram.total").(float64),
			MemoryUnit:  "KiB",
			MemoryPerc:  fixtureValue(t, sys, "system.memory.ram.%used").(float64),
		}
		if out != want {
			t.Errorf("structured values = %+v, want %+v", out, want)
		}
	})
}
