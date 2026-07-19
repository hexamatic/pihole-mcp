package main

import (
	"os"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestRenderMatchesCommittedReference regenerates the tool reference through
// the same two-pass registration main() uses and compares it against the
// committed docs/TOOLS.md. This is the CI drift check as a unit test: if it
// fails, run `just docs-gen`.
func TestRenderMatchesCommittedReference(t *testing.T) {
	tools.ResetCatalogue()
	t.Cleanup(tools.ResetCatalogue)

	single := register(1)
	if len(single) == 0 {
		t.Fatal("single-instance registration recorded no tools")
	}
	singleNames := make(map[string]bool, len(single))
	for _, tool := range single {
		singleNames[tool.Name] = true
	}

	tools.ResetCatalogue()
	multi := register(2)
	var multiOnly []mcp.Tool
	for _, tool := range multi {
		if !singleNames[tool.Name] {
			multiOnly = append(multiOnly, tool)
		}
	}
	if len(multiOnly) != 2 {
		t.Fatalf("multi-instance-only tools = %d, want 2 (instance_diff, instance_sync)", len(multiOnly))
	}

	got := render(single, multiOnly)

	want, err := os.ReadFile("../../docs/TOOLS.md")
	if err != nil {
		t.Fatalf("read committed reference: %v", err)
	}
	if got != string(want) {
		t.Error("rendered output differs from docs/TOOLS.md — run `just docs-gen`")
	}
}

func TestCategory(t *testing.T) {
	tests := []struct{ name, want string }{
		{"pihole_dns_get_blocking", "DNS Blocking"},
		{"pihole_instance_diff", "Multi-Instance"},
		{"pihole_padd", "Dashboard"},
		{"pihole", "Other"},
		{"pihole_zzz_new", "Other"},
	}
	for _, tt := range tests {
		if got := category(tt.name); got != tt.want {
			t.Errorf("category(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestPropType(t *testing.T) {
	if got := propType(map[string]any{"type": "string"}); got != "string" {
		t.Errorf("plain type = %q", got)
	}
	if got := propType(map[string]any{}); got != "any" {
		t.Errorf("missing type = %q", got)
	}
	got := propType(map[string]any{"type": "string", "enum": []any{"plan", "apply"}})
	if got != "string (`plan`, `apply`)" {
		t.Errorf("enum type = %q", got)
	}
}

func TestPropDescription(t *testing.T) {
	if got := propDescription(map[string]any{"description": "A thing."}); got != "A thing." {
		t.Errorf("plain = %q", got)
	}
	got := propDescription(map[string]any{"description": "A thing", "default": float64(25)})
	if got != "A thing. Default: `25`." {
		t.Errorf("with default = %q", got)
	}
	if got := propDescription(map[string]any{"default": true}); got != "Default: `true`." {
		t.Errorf("default only = %q", got)
	}
}

func TestCell(t *testing.T) {
	if got := cell("a|b\nc"); got != "a\\|b c" {
		t.Errorf("cell = %q", got)
	}
}
