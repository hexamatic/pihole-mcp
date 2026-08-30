package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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

// summaryWithTotal builds a fake Pi-hole whose only route is a stats summary
// reporting the given query total. It returns the recorder rather than a bare
// http.Handler so a test can ask which of the two instances was actually
// contacted, which is the one thing multi-instance routing can get wrong
// without any visible effect on the reply.
func summaryWithTotal(total int) *recorder {
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
	primary, secondary := summaryWithTotal(100), summaryWithTotal(200)
	reg := twoInstanceRegistry(t, primary, secondary)
	res := callRaw(t, statsSummaryHandler(reg), nil)
	if !strings.Contains(resultText(res), "100") {
		t.Errorf("default instance should hit primary (100), got: %s", resultText(res))
	}

	// Reading 100 back only proves whose answer was rendered. A registry that
	// asked both Pi-holes and then kept the default's reply would look exactly
	// the same here, while spending a session seat on an instance the caller
	// never named and, for a write tool, applying the change twice.
	primary.AssertCount(t, "GET", "/stats/summary", 1)
	secondary.AssertNone(t, "", "")
}

func TestInstance_SelectByName(t *testing.T) {
	primary, secondary := summaryWithTotal(100), summaryWithTotal(200)
	reg := twoInstanceRegistry(t, primary, secondary)
	res := callRaw(t, statsSummaryHandler(reg), map[string]any{"instance": "secondary"})
	if !strings.Contains(resultText(res), "200") {
		t.Errorf("instance=secondary should hit 200, got: %s", resultText(res))
	}

	// The naming has to be exclusive, not merely inclusive. Nothing in the
	// rendered text distinguishes "asked secondary" from "asked both and
	// preferred secondary".
	secondary.AssertCount(t, "GET", "/stats/summary", 1)
	primary.AssertNone(t, "", "")
}

// Every routing case above drives pihole_stats_summary, which is a read. In a
// multi-instance setup the worst thing this server can do is land a
// DESTRUCTIVE write on the Pi-hole the caller did not name, and until now
// nothing pinned that: every CRUD test uses a single fake, so it cannot tell
// one instance from another by construction.
//
// The load-bearing half of each case is the AssertNone on the untargeted
// instance. Asserting only that the named one was written to would pass just
// as happily if BOTH were written to, which is the actual failure mode.
func TestInstance_DestructiveWritesOnlyTouchTheNamedInstance(t *testing.T) {
	for _, tc := range []struct {
		name    string
		routes  map[string]any
		handler func(*pihole.Registry) server.ToolHandlerFunc
		args    map[string]any
		method  string
		path    string
	}{
		{
			name:    "domains_delete",
			routes:  map[string]any{"/domains/deny/exact/example.com": map[string]any{}},
			handler: domainsDeleteHandler,
			args:    map[string]any{"type": "deny", "kind": "exact", "domain": "example.com"},
			method:  "DELETE", path: "/domains/deny/exact/example.com",
		},
		{
			name:    "groups_delete",
			routes:  map[string]any{"/groups/kids": map[string]any{}},
			handler: groupsDeleteHandler,
			args:    map[string]any{"name": "kids"},
			method:  "DELETE", path: "/groups/kids",
		},
		{
			name:    "lists_delete",
			routes:  map[string]any{"/lists/https://example.com/list.txt": map[string]any{}},
			handler: listsDeleteHandler,
			args:    map[string]any{"address": "https://example.com/list.txt", "type": "block"},
			method:  "DELETE", path: "/lists/https://example.com/list.txt",
		},
		{
			name:    "clients_delete",
			routes:  map[string]any{"/clients/192.168.1.50": map[string]any{}},
			handler: clientsDeleteHandler,
			args:    map[string]any{"client": "192.168.1.50"},
			method:  "DELETE", path: "/clients/192.168.1.50",
		},
		{
			name:    "config_set",
			routes:  map[string]any{"PATCH /config": map[string]any{"config": map[string]any{}}},
			handler: configSetHandler,
			args:    map[string]any{"config": `{"dns":{"blocking":{"active":false}}}`},
			method:  "PATCH", path: "/config",
		},
		{
			name:    "dns_set_blocking",
			routes:  map[string]any{"/dns/blocking": map[string]any{"blocking": "disabled"}},
			handler: dnsSetBlockingHandler,
			args:    map[string]any{"blocking": false},
			method:  "POST", path: "/dns/blocking",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary, secondary := piholeHandler(tc.routes), piholeHandler(tc.routes)
			reg := twoInstanceRegistry(t, primary, secondary)

			args := map[string]any{"instance": "secondary"}
			for k, v := range tc.args {
				args[k] = v
			}
			res := callRaw(t, tc.handler(reg), args)
			if res.IsError {
				t.Fatalf("tool error: %v", res.Content)
			}

			secondary.Only(t, tc.method, tc.path)
			// The Pi-hole the caller never named must be untouched, down to
			// not even having been logged into.
			primary.AssertNone(t, "", "")
		})
	}
}

func TestInstance_UnknownNameErrors(t *testing.T) {
	primary, secondary := summaryWithTotal(100), summaryWithTotal(200)
	reg := twoInstanceRegistry(t, primary, secondary)
	res := callRaw(t, statsSummaryHandler(reg), map[string]any{"instance": "ghost"})
	if !res.IsError {
		t.Fatal("expected error for unknown instance")
	}

	// An unrecognised name has to be refused before anything is sent. A
	// resolver that fell through to the default, noticed afterwards and then
	// reported the error would still satisfy the check above, having already
	// run the tool against a Pi-hole the caller did not ask for.
	primary.AssertNone(t, "", "")
	secondary.AssertNone(t, "", "")
}

func TestInstance_AllAggregatesReadOnly(t *testing.T) {
	primary, secondary := summaryWithTotal(100), summaryWithTotal(200)
	reg := twoInstanceRegistry(t, primary, secondary)
	tool := mcp.NewTool("x", mcp.WithReadOnlyHintAnnotation(true))
	wrapped := instanceAware(reg, tool, statsSummaryHandler(reg))

	res := callRaw(t, wrapped, map[string]any{"instance": "all"})
	text := resultText(res)
	for _, want := range []string{"### instance: primary", "### instance: secondary", "100", "200"} {
		if !strings.Contains(text, want) {
			t.Errorf("aggregate missing %q, got:\n%s", want, text)
		}
	}

	// Fan-out is one call per instance. A retry or a duplicated dispatch would
	// produce a byte-identical aggregate, so the section headings above cannot
	// see it.
	primary.AssertCount(t, "GET", "/stats/summary", 1)
	secondary.AssertCount(t, "GET", "/stats/summary", 1)
}

func TestInstance_AllRejectedForWrites(t *testing.T) {
	primary, secondary := summaryWithTotal(100), summaryWithTotal(200)
	reg := twoInstanceRegistry(t, primary, secondary)
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

	// The refusal is worth nothing unless it lands before the fan-out. If the
	// guard ever moved after the dispatch, a destructive tool would already
	// have run against every Pi-hole on the network by the time the caller was
	// told the mode was not supported, and the error result would read the same.
	primary.AssertNone(t, "", "")
	secondary.AssertNone(t, "", "")
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
	primary, secondary := summaryWithTotal(100), summaryWithTotal(200)
	reg := twoInstanceRegistry(t, primary, secondary)
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

	// The prefix is a label the wrapper writes about the instance it believes
	// it targeted, so it is capable of naming one Pi-hole over another's data.
	// One call each, in that order, is what makes the label true; a prefix that
	// lies is worse than no prefix at all, because the reader now trusts it.
	primary.AssertCount(t, "GET", "/stats/summary", 1)
	secondary.AssertCount(t, "GET", "/stats/summary", 1)
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
