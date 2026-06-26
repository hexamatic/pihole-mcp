package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
)

// twoInstanceRegistry spins up two Pi-hole test servers and returns a registry
// with "primary" (default) first and "secondary" second.
func twoInstanceRegistry(t *testing.T, h1, h2 http.Handler) *pihole.Registry {
	t.Helper()
	s1 := httptest.NewServer(h1)
	t.Cleanup(s1.Close)
	s2 := httptest.NewServer(h2)
	t.Cleanup(s2.Close)
	return pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "primary", URL: s1.URL, Password: "test"},
		{Name: "secondary", URL: s2.URL, Password: "test"},
	})
}

func summaryWithTotal(total int) http.Handler {
	return piholeHandler(map[string]any{
		"/stats/summary": map[string]any{
			"queries": map[string]any{"total": total, "blocked": 0, "percent_blocked": 0.0},
			"clients": map[string]any{"active": 1, "total": 1},
			"gravity": map[string]any{"domains_being_blocked": 1},
		},
	})
}

func callRaw(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

func TestInstance_DefaultHitsPrimary(t *testing.T) {
	reg := twoInstanceRegistry(t, summaryWithTotal(100), summaryWithTotal(200))
	res := callRaw(t, statsSummaryHandler(reg), nil)
	if !strings.Contains(resultText(res), "100") {
		t.Errorf("default instance should hit primary (100), got: %s", resultText(res))
	}
}

func TestInstance_SelectByName(t *testing.T) {
	reg := twoInstanceRegistry(t, summaryWithTotal(100), summaryWithTotal(200))
	res := callRaw(t, statsSummaryHandler(reg), map[string]any{"instance": "secondary"})
	if !strings.Contains(resultText(res), "200") {
		t.Errorf("instance=secondary should hit 200, got: %s", resultText(res))
	}
}

func TestInstance_UnknownNameErrors(t *testing.T) {
	reg := twoInstanceRegistry(t, summaryWithTotal(100), summaryWithTotal(200))
	res := callRaw(t, statsSummaryHandler(reg), map[string]any{"instance": "ghost"})
	if !res.IsError {
		t.Fatal("expected error for unknown instance")
	}
}

func TestInstance_AllAggregatesReadOnly(t *testing.T) {
	reg := twoInstanceRegistry(t, summaryWithTotal(100), summaryWithTotal(200))
	tool := mcp.NewTool("x", mcp.WithReadOnlyHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	res := callRaw(t, wrapped, map[string]any{"instance": "all"})
	text := resultText(res)
	for _, want := range []string{"### instance: primary", "### instance: secondary", "100", "200"} {
		if !strings.Contains(text, want) {
			t.Errorf("aggregate missing %q, got:\n%s", want, text)
		}
	}
}

func TestInstance_AllRejectedForWrites(t *testing.T) {
	reg := twoInstanceRegistry(t, summaryWithTotal(100), summaryWithTotal(200))
	// A tool with no read-only annotation is treated as state-changing.
	tool := mcp.NewTool("y", mcp.WithDestructiveHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	res := callRaw(t, wrapped, map[string]any{"instance": "all"})
	if !res.IsError {
		t.Fatal("expected instance=all to be rejected for a state-changing tool")
	}
	if !strings.Contains(resultText(res), "not supported") {
		t.Errorf("unexpected rejection message: %s", resultText(res))
	}
}

func TestInstance_AllPartialFailureIsReported(t *testing.T) {
	// Secondary has no /stats/summary route → that instance errors, but the
	// aggregate must still return the primary's data, not fail wholesale.
	reg := twoInstanceRegistry(t, summaryWithTotal(100), piholeHandler(map[string]any{}))
	tool := mcp.NewTool("x", mcp.WithReadOnlyHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	res := callRaw(t, wrapped, map[string]any{"instance": "all"})
	text := resultText(res)
	if !strings.Contains(text, "100") {
		t.Errorf("expected primary data in aggregate, got:\n%s", text)
	}
	if !strings.Contains(text, "### instance: secondary") {
		t.Errorf("expected secondary section header even on failure, got:\n%s", text)
	}
}

func TestInstance_AllReturnsStructuredEnvelope(t *testing.T) {
	reg := twoInstanceRegistry(t, summaryWithTotal(100), summaryWithTotal(200))
	tool := mcp.NewTool("x", mcp.WithReadOnlyHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	res := callRaw(t, wrapped, map[string]any{"instance": "all"})
	out, ok := res.StructuredContent.(AggregateOutput)
	if !ok {
		t.Fatalf("expected AggregateOutput structured content, got %T", res.StructuredContent)
	}
	if out.Summary.Total != 2 || out.Summary.OK != 2 || out.Summary.Failed != 0 {
		t.Errorf("unexpected summary: %+v", out.Summary)
	}
	if len(out.Instances) != 2 {
		t.Fatalf("expected 2 instance results, got %d", len(out.Instances))
	}
	// Deterministic declaration order.
	if out.Instances[0].Instance != "primary" || out.Instances[1].Instance != "secondary" {
		t.Errorf("instance order not preserved: %+v", out.Instances)
	}
	for _, ir := range out.Instances {
		if !ir.OK {
			t.Errorf("instance %s unexpectedly failed: %s", ir.Instance, ir.Error)
		}
	}
}

func TestInstance_AllPartialFailureStructured(t *testing.T) {
	reg := twoInstanceRegistry(t, summaryWithTotal(100), piholeHandler(map[string]any{}))
	tool := mcp.NewTool("x", mcp.WithReadOnlyHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	res := callRaw(t, wrapped, map[string]any{"instance": "all"})
	if res.IsError {
		t.Fatal("partial failure must not fail the whole aggregate")
	}
	out := res.StructuredContent.(AggregateOutput)
	if out.Summary.OK != 1 || out.Summary.Failed != 1 {
		t.Errorf("expected 1 ok / 1 failed, got %+v", out.Summary)
	}
}

func TestInstance_AllFailsWhenEveryInstanceFails(t *testing.T) {
	reg := twoInstanceRegistry(t, piholeHandler(map[string]any{}), piholeHandler(map[string]any{}))
	tool := mcp.NewTool("x", mcp.WithReadOnlyHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	res := callRaw(t, wrapped, map[string]any{"instance": "all"})
	if !res.IsError {
		t.Fatal("expected an error result when every instance fails")
	}
}

func TestInstance_SingleTargetCarriesProvenance(t *testing.T) {
	reg := twoInstanceRegistry(t, summaryWithTotal(100), summaryWithTotal(200))
	tool := mcp.NewTool("x", mcp.WithReadOnlyHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	// Default target → provenance names the default instance.
	res := callRaw(t, wrapped, nil)
	if !strings.HasPrefix(resultText(res), "instance: primary\n") {
		t.Errorf("default call missing provenance prefix, got: %q", resultText(res))
	}

	// Named target → provenance names that instance.
	res = callRaw(t, wrapped, map[string]any{"instance": "secondary"})
	if !strings.HasPrefix(resultText(res), "instance: secondary\n") {
		t.Errorf("named call missing provenance prefix, got: %q", resultText(res))
	}
}

func TestInstance_SingleInstanceHasNoProvenance(t *testing.T) {
	c := newTestClient(t, summaryWithTotal(100))
	reg := pihole.SingleRegistry(c)
	tool := mcp.NewTool("x", mcp.WithReadOnlyHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	res := callRaw(t, wrapped, nil)
	if strings.Contains(resultText(res), "instance:") {
		t.Errorf("single-instance output should not carry a provenance prefix, got: %q", resultText(res))
	}
}

func TestAddInstanceParam_OnlyWhenMultiInstance(t *testing.T) {
	// Single instance: schema must NOT advertise the instance argument.
	single := pihole.SingleRegistry(pihole.New("http://x", "pw"))
	toolS := mcp.NewTool("x")
	if single.Len() > 1 {
		addInstanceParam(&toolS, single)
	}
	if _, ok := toolS.InputSchema.Properties[instanceArg]; ok {
		t.Error("single-instance tool should not advertise the instance argument")
	}

	// Multi instance: schema must advertise it.
	multi := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "primary", URL: "http://a", Password: "p"},
		{Name: "secondary", URL: "http://b", Password: "p"},
	})
	toolM := mcp.NewTool("y")
	addInstanceParam(&toolM, multi)
	if _, ok := toolM.InputSchema.Properties[instanceArg]; !ok {
		t.Error("multi-instance tool should advertise the instance argument")
	}
}
