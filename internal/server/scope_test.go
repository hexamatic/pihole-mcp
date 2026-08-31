package server

import (
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/hexamatic/pihole-mcp/internal/toolsets"
	"github.com/mark3labs/mcp-go/server"
)

// Tool counts as registered. Kept here rather than derived from the catalogue
// so that a filter which silently dropped or duplicated tools cannot move the
// expectation with it.
const (
	toolsSingleInstance    = 82
	toolsMultiInstance     = 84
	readOnlySingleInstance = 49
	readOnlyMultiInstance  = 50
)

// newScopedDispatchServer is newDispatchServer with scoping options applied.
//
// Every assertion here goes over the wire through dispatch rather than through
// srv.ListTools(), which returns a copy of the raw tool map and never consults
// a filter. A test built on ListTools would pass against a filter that was
// written correctly and then never attached to the server, which is precisely
// the mistake worth catching.
func newScopedDispatchServer(t *testing.T, opts []Option, fakes ...*piholefake.Fake) *server.MCPServer {
	t.Helper()
	instances := make([]pihole.InstanceConfig, len(fakes))
	for i, f := range fakes {
		instances[i] = pihole.InstanceConfig{Name: instanceNames[i], URL: f.URL(), Password: "x"}
	}
	reg := pihole.NewRegistry(instances)
	t.Cleanup(reg.Close)
	return New(reg, opts...)
}

// listedTools returns the tool names and their readOnlyHint annotations from a
// real tools/list response.
func listedTools(t *testing.T, srv *server.MCPServer) map[string]bool {
	t.Helper()
	res := dispatchResult(t, dispatch(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	raw, ok := res["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list result has no tools array: %#v", res)
	}
	out := make(map[string]bool, len(raw))
	for _, entry := range raw {
		tool, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("tool entry is not an object: %#v", entry)
		}
		name, _ := tool["name"].(string)
		ann, _ := tool["annotations"].(map[string]any)
		readOnly, _ := ann["readOnlyHint"].(bool)
		out[name] = readOnly
	}
	return out
}

// TestUnscopedServerListsEveryTool is the backward-compatibility guard. Eight
// releases have shipped the full surface unconditionally, so the default must
// keep doing that: a curated default would silently shrink the tool surface of
// every existing zero-config install on upgrade.
//
// This passes today by definition. It exists to fail if a future change ever
// narrows the default.
func TestUnscopedServerListsEveryTool(t *testing.T) {
	single := listedTools(t, newScopedDispatchServer(t, nil, newDispatchFake(t)))
	if len(single) != toolsSingleInstance {
		t.Errorf("single instance listed %d tools, want %d", len(single), toolsSingleInstance)
	}
	multi := listedTools(t, newScopedDispatchServer(t, nil, newDispatchFake(t), newDispatchFake(t)))
	if len(multi) != toolsMultiInstance {
		t.Errorf("multi instance listed %d tools, want %d", len(multi), toolsMultiInstance)
	}
}

// TestReadOnlyModeListsOnlyReadOnlyTools asserts both halves, and both are
// required. The universal claim alone passes vacuously against a filter that
// returns an empty slice; the count alone passes against a filter that drops
// the wrong tools.
func TestReadOnlyModeListsOnlyReadOnlyTools(t *testing.T) {
	for _, tc := range []struct {
		name      string
		instances int
		want      int
	}{
		{"single instance", 1, readOnlySingleInstance},
		{"multi instance", 2, readOnlyMultiInstance},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakes := make([]*piholefake.Fake, tc.instances)
			for i := range fakes {
				fakes[i] = newDispatchFake(t)
			}
			listed := listedTools(t, newScopedDispatchServer(t, []Option{WithReadOnly(true)}, fakes...))

			for name, readOnly := range listed {
				if !readOnly {
					t.Errorf("%s is listed in read-only mode but is not annotated read-only", name)
				}
			}
			if len(listed) != tc.want {
				t.Errorf("listed %d tools, want %d", len(listed), tc.want)
			}
		})
	}
}

// TestReadOnlyModeRejectsWriteToolCall is the half that makes read-only an
// access control boundary rather than a visibility hint. A client working from
// a cached listing, or a model that produced the name from memory, is refused.
func TestReadOnlyModeRejectsWriteToolCall(t *testing.T) {
	srv := newScopedDispatchServer(t, []Option{WithReadOnly(true)}, newDispatchFake(t))

	envelope := dispatch(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
		"params":{"name":"pihole_domains_add","arguments":{"domain":"example.com","type":"deny","kind":"exact"}}}`)

	if _, present := envelope["error"]; !present {
		t.Fatalf("calling a write tool in read-only mode succeeded: %#v", envelope["result"])
	}
}

// TestReadOnlyModeReachesDirectlyRegisteredTools covers the two sync tools,
// which register through s.AddTool directly rather than through addTool. A gate
// built into addTool would leave pihole_instance_sync exposed, which is the
// whole reason the filter seam was chosen over gating registration.
func TestReadOnlyModeReachesDirectlyRegisteredTools(t *testing.T) {
	listed := listedTools(t, newScopedDispatchServer(t,
		[]Option{WithReadOnly(true)}, newDispatchFake(t), newDispatchFake(t)))

	if _, ok := listed["pihole_instance_diff"]; !ok {
		t.Error("pihole_instance_diff is read-only and should still be listed")
	}
	if _, ok := listed["pihole_instance_sync"]; ok {
		t.Error("pihole_instance_sync writes and must not be listed in read-only mode")
	}
}

func TestToolsetsSelectionListsOnlySelectedToolsets(t *testing.T) {
	listed := listedTools(t, newScopedDispatchServer(t,
		[]Option{WithToolsets([]string{"domains"})}, newDispatchFake(t)))

	// A stray tool from another toolset is as much a bug as a missing one, so
	// check membership rather than only the count.
	for name := range listed {
		if ts, _ := toolsets.Of(name); ts != "domains" {
			t.Errorf("%s is in toolset %q, not domains", name, ts)
		}
	}
	// Five domains tools plus pihole_search_domains, the one merge in the table.
	if len(listed) != 6 {
		t.Errorf("listed %d tools, want 6", len(listed))
	}
	if _, ok := listed["pihole_search_domains"]; !ok {
		t.Error("pihole_search_domains belongs to the domains toolset and should be listed")
	}
}

// TestInstanceToolsetValidOnSingleInstance pins that selecting a toolset whose
// tools happen not to be registered is not an error. Absence by instance count
// is expected behaviour, not a misconfiguration.
func TestInstanceToolsetValidOnSingleInstance(t *testing.T) {
	listed := listedTools(t, newScopedDispatchServer(t,
		[]Option{WithToolsets([]string{"instance"})}, newDispatchFake(t)))

	if len(listed) != 0 {
		t.Errorf("listed %d tools, want 0: the sync tools need a second instance", len(listed))
	}
}

// TestToolsetsAllMatchesUnscoped proves "all" is a true synonym for unset
// rather than a separate, subtly narrower path through the filter.
func TestToolsetsAllMatchesUnscoped(t *testing.T) {
	unscoped := listedTools(t, newScopedDispatchServer(t, nil, newDispatchFake(t)))
	all := listedTools(t, newScopedDispatchServer(t,
		[]Option{WithToolsets([]string{"all"})}, newDispatchFake(t)))

	if len(unscoped) != len(all) {
		t.Fatalf("all listed %d tools, unscoped listed %d", len(all), len(unscoped))
	}
	for name := range unscoped {
		if _, ok := all[name]; !ok {
			t.Errorf("%s is listed unscoped but missing under toolsets=all", name)
		}
	}
}

// TestReadOnlyIntersectsToolsets discriminates an intersection from a union,
// which is the one precedence an implementation could plausibly invert. The
// actions toolset has no read-only members at all, so a union would list four
// tools where the correct answer is none.
func TestReadOnlyIntersectsToolsets(t *testing.T) {
	actions := listedTools(t, newScopedDispatchServer(t,
		[]Option{WithReadOnly(true), WithToolsets([]string{"actions"})}, newDispatchFake(t)))
	if len(actions) != 0 {
		t.Errorf("read-only + actions listed %d tools, want 0: every action writes", len(actions))
	}

	network := listedTools(t, newScopedDispatchServer(t,
		[]Option{WithReadOnly(true), WithToolsets([]string{"network"})}, newDispatchFake(t)))
	if len(network) != 5 {
		t.Errorf("read-only + network listed %d tools, want 5 (all but pihole_network_delete_device)", len(network))
	}
}

// TestReadOnlyInstructionsNameNoWriteTool checks the instruction string a
// read-only deployment injects into every conversation names no tool it has
// hidden. Computed from the catalogue rather than from a fixed list, so it
// stays true as the instructions grow.
func TestReadOnlyInstructionsNameNoWriteTool(t *testing.T) {
	readOnly := map[string]bool{}
	for _, tool := range tools.Catalogue() {
		readOnly[tool.Name] = tools.IsReadOnly(tool)
	}
	if len(readOnly) == 0 {
		t.Fatal("the tool catalogue is empty; another test must register first")
	}

	for _, name := range toolNamesIn(instructions(true)) {
		ro, known := readOnly[name]
		if !known {
			t.Errorf("read-only instructions name %s, which is not a registered tool", name)
			continue
		}
		if !ro {
			t.Errorf("read-only instructions tell the model to call %s, which read-only mode rejects", name)
		}
	}
}

// TestInstructionsRewordedFromTheOverclaim guards the specific sentence that
// was false for most of the surface: 15 tools take detail and 20 take format,
// out of 84.
func TestInstructionsRewordedFromTheOverclaim(t *testing.T) {
	text := instructions(false)
	if strings.Contains(text, "Tools accept optional") {
		t.Error("instructions still claim every tool accepts detail and format")
	}
	if !strings.Contains(text, "Some tools accept optional") {
		t.Error("instructions no longer describe detail and format at all")
	}
}

// toolNamesIn extracts pihole_-prefixed tool names from prose, ignoring the
// wildcard form the instructions use for tool families.
func toolNamesIn(text string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	}) {
		// The instructions refer to a family as pihole_stats_database_*, which
		// tokenises to a trailing underscore and names no single tool.
		if strings.HasPrefix(field, "pihole_") && !strings.HasSuffix(field, "_") {
			out = append(out, field)
		}
	}
	return out
}
