package tools

import (
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/server"
)

// dummyRegistry builds a registry of n instances with unreachable URLs. Tool
// registration never makes a request, so the URLs only need to be well-formed.
func dummyRegistry(n int) *pihole.Registry {
	instances := make([]pihole.InstanceConfig, n)
	for i := range instances {
		instances[i] = pihole.InstanceConfig{
			Name:     string(rune('a'+i)) + "-instance",
			URL:      "http://127.0.0.1:1",
			Password: "x",
		}
	}
	return pihole.NewRegistry(instances)
}

// TestToolAnnotationInvariants locks in the annotation hygiene across every
// registered tool: each has a human-readable title, and a read-only tool is
// never also flagged destructive or open-world (which mcp-go's NewTool defaults
// would otherwise leave set).
func TestToolAnnotationInvariants(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.0")
	RegisterAll(srv, dummyRegistry(2))

	tools := srv.ListTools()
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}

	for name, st := range tools {
		ann := st.Tool.Annotations
		if ann.Title == "" {
			t.Errorf("%s: missing human-readable title annotation", name)
		}
		if ann.ReadOnlyHint != nil && *ann.ReadOnlyHint {
			if ann.DestructiveHint != nil && *ann.DestructiveHint {
				t.Errorf("%s: read-only tool must not be marked destructive", name)
			}
			if ann.OpenWorldHint != nil && *ann.OpenWorldHint {
				t.Errorf("%s: read-only tool must not be marked open-world", name)
			}
		}
	}
}

// TestDestructiveHintMatrix locks in the specific hint each CRUD family's add
// and update tool carries: additive tools are not destructive (nothing
// existing is overwritten or removed), update tools are (a PUT replaces an
// existing row). A regression here is the exact shape of the bug
// normaliseReadOnlyAnnotations exists to prevent — a tool's own annotation
// disagreeing with what it does.
func TestDestructiveHintMatrix(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.0")
	RegisterAll(srv, dummyRegistry(1))
	tools := srv.ListTools()

	notDestructive := []string{
		"pihole_domains_add",
		"pihole_groups_add",
		"pihole_clients_add",
		"pihole_lists_add",
		"pihole_config_add_value",
	}
	destructive := []string{
		"pihole_domains_update",
		"pihole_groups_update",
		"pihole_clients_update",
		"pihole_lists_update",
	}

	for _, name := range notDestructive {
		st, ok := tools[name]
		if !ok {
			t.Errorf("%s: not registered", name)
			continue
		}
		if h := st.Tool.Annotations.DestructiveHint; h == nil || *h {
			t.Errorf("%s: expected destructiveHint=false, got %v", name, h)
		}
	}
	for _, name := range destructive {
		st, ok := tools[name]
		if !ok {
			t.Errorf("%s: not registered", name)
			continue
		}
		if h := st.Tool.Annotations.DestructiveHint; h == nil || !*h {
			t.Errorf("%s: expected destructiveHint=true, got %v", name, h)
		}
	}
}

// TestSyncToolsGatedByInstanceCount verifies the diff/sync tools appear only
// when more than one instance is configured.
func TestSyncToolsGatedByInstanceCount(t *testing.T) {
	multi := server.NewMCPServer("test", "0.0.0")
	RegisterAll(multi, dummyRegistry(2))
	if multi.GetTool("pihole_instance_diff") == nil || multi.GetTool("pihole_instance_sync") == nil {
		t.Error("expected diff and sync tools with multiple instances")
	}

	single := server.NewMCPServer("test", "0.0.0")
	RegisterAll(single, dummyRegistry(1))
	if single.GetTool("pihole_instance_diff") != nil || single.GetTool("pihole_instance_sync") != nil {
		t.Error("diff/sync tools must not be registered for a single instance")
	}
}

// TestInstanceArgGatedByInstanceCount verifies the shared instance selector is
// only advertised in a multi-instance setup.
func TestInstanceArgGatedByInstanceCount(t *testing.T) {
	single := server.NewMCPServer("test", "0.0.0")
	RegisterAll(single, dummyRegistry(1))
	st := single.GetTool("pihole_stats_summary")
	if st == nil {
		t.Fatal("pihole_stats_summary not registered")
	}
	if _, ok := st.Tool.InputSchema.Properties[instanceArg]; ok {
		t.Error("single-instance tool must not advertise the instance argument")
	}

	multi := server.NewMCPServer("test", "0.0.0")
	RegisterAll(multi, dummyRegistry(2))
	st = multi.GetTool("pihole_stats_summary")
	if _, ok := st.Tool.InputSchema.Properties[instanceArg]; !ok {
		t.Error("multi-instance tool must advertise the instance argument")
	}
}
