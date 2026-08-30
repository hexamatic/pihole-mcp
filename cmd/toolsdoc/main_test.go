package main

import (
	"os"
	"strings"
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

// A bound the schema declares and the reference does not show is a constraint
// callers only discover by tripping over it.
func TestPropTypeBounds(t *testing.T) {
	cases := []struct {
		name string
		prop map[string]any
		want string
	}{
		{"both bounds", map[string]any{"type": "number", "minimum": 1, "maximum": 50}, "number (1 to 50)"},
		{"minimum only", map[string]any{"type": "number", "minimum": 0}, "number (min 0)"},
		{"maximum only", map[string]any{"type": "number", "maximum": 1000}, "number (max 1000)"},
		{"no bounds", map[string]any{"type": "number"}, "number"},
		// A JSON round trip turns every bound into a float64, which must not
		// render as "50.0".
		{"float bounds", map[string]any{"type": "number", "minimum": float64(1), "maximum": float64(50)}, "number (1 to 50)"},
		{"int64 bound", map[string]any{"type": "number", "minimum": int64(2)}, "number (min 2)"},
		{"fractional bound", map[string]any{"type": "number", "minimum": 0.5}, "number (min 0.5)"},
		// An enum is already an exhaustive constraint, so bounds add nothing.
		{"enum wins", map[string]any{"type": "string", "enum": []any{"a"}, "minimum": 1}, "string (`a`)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := propType(tc.prop); got != tc.want {
				t.Errorf("propType = %q, want %q", got, tc.want)
			}
		})
	}
}

// The generated reference has to show the bounds the real tools declare, or
// this whole exercise is a unit test of a function nothing reaches.
func TestRenderedDocsCarryDeclaredBounds(t *testing.T) {
	out := render(registerAll())
	for _, want := range []string{
		"| `count` | number (1 to 50) |",
		"| `length` | number (1 to 100) |",
		"| `limit` | number (0 to 1000) |",
		"| `offset` | number (min 0) |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated reference is missing %q", want)
		}
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

// e2eFixture mirrors the shapes the real script uses: the helper definitions
// and the usage comment (neither of which is a call), a plain call_tool, a
// call with arguments and a label, call_multi, the call_tool_expect variant,
// a commented-out call, and a comment that merely names a tool.
const e2eFixture = `#!/usr/bin/env bash
# call_tool <name> [args] [label] [expect_error]
call_tool() {
    :
}
call_multi() {
    :
}

call_tool "pihole_padd"
call_tool "pihole_stats_summary" '{"detail":"minimal"}' "stats_summary (minimal)"
call_tool_expect "pihole_config_set" '{"config":"[1,2,3]"}' "rejects non-object" error
call_multi "pihole_instance_diff" '{"source":"a","target":"b"}'
    call_tool "pihole_logs_dns"   # indented, with a trailing comment
# call_tool "pihole_commented_out"
# pihole_network_delete_device intentionally not exercised.
`

func TestParseE2EToolNames(t *testing.T) {
	got := parseE2EToolNames(e2eFixture)

	want := []string{
		"pihole_padd",
		"pihole_stats_summary",
		"pihole_config_set",
		"pihole_instance_diff",
		"pihole_logs_dns",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("parseE2EToolNames did not extract %q", name)
		}
	}

	// A commented-out call is not coverage, and neither is a comment that
	// only mentions a tool by name.
	for _, name := range []string{"pihole_commented_out", "pihole_network_delete_device"} {
		if got[name] {
			t.Errorf("parseE2EToolNames wrongly extracted %q from a comment", name)
		}
	}

	if len(got) != len(want) {
		t.Errorf("extracted %d names %v, want exactly %d", len(got), got, len(want))
	}
}

// TestReportE2ECoverageFlagsUncovered is the point of the check: a registered
// tool that neither the script nor the skip list accounts for must fail.
func TestReportE2ECoverageFlagsUncovered(t *testing.T) {
	registered := []string{"pihole_covered", "pihole_skipped", "pihole_forgotten"}
	covered := map[string]bool{"pihole_covered": true}
	skips := map[string]string{"pihole_skipped": "Destroys state the rest of the run reads."}

	var buf strings.Builder
	if reportE2ECoverage(registered, covered, skips, "script.sh", &buf) {
		t.Fatal("check passed with an uncovered tool; it must fail")
	}
	out := buf.String()
	if !strings.Contains(out, "pihole_forgotten") {
		t.Errorf("report does not name the uncovered tool:\n%s", out)
	}
	if strings.Contains(out, "  pihole_covered\n") {
		t.Errorf("report lists a covered tool as uncovered:\n%s", out)
	}
	if !strings.Contains(out, "Destroys state the rest of the run reads.") {
		t.Errorf("report omits the skip reason:\n%s", out)
	}
}

func TestReportE2ECoveragePasses(t *testing.T) {
	registered := []string{"pihole_covered", "pihole_skipped"}
	covered := map[string]bool{"pihole_covered": true}
	skips := map[string]string{"pihole_skipped": "Takes minutes and needs internet access."}

	var buf strings.Builder
	if !reportE2ECoverage(registered, covered, skips, "script.sh", &buf) {
		t.Fatalf("check failed when every tool is covered or skipped:\n%s", buf.String())
	}
}

// A skip must not outlive its reason: an entry naming a tool that no longer
// exists, or one the script now exercises, fails the check.
func TestReportE2ECoverageFlagsStaleSkips(t *testing.T) {
	t.Run("unregistered", func(t *testing.T) {
		var buf strings.Builder
		ok := reportE2ECoverage([]string{"pihole_covered"}, map[string]bool{"pihole_covered": true},
			map[string]string{"pihole_gone": "Reason from a tool that has since been removed."}, "script.sh", &buf)
		if ok {
			t.Fatalf("check passed with a skip for an unregistered tool:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "pihole_gone") {
			t.Errorf("report does not name the stale skip:\n%s", buf.String())
		}
	})

	t.Run("now covered", func(t *testing.T) {
		var buf strings.Builder
		ok := reportE2ECoverage([]string{"pihole_covered"}, map[string]bool{"pihole_covered": true},
			map[string]string{"pihole_covered": "Reason that no longer applies."}, "script.sh", &buf)
		if ok {
			t.Fatalf("check passed with a skip for a tool the script exercises:\n%s", buf.String())
		}
	})
}

// Every skip entry must name a real tool and state a reason a maintainer can
// judge, so the list cannot be padded with a bare name or an empty string.
func TestE2ESkipsAreJustifiedAndRegistered(t *testing.T) {
	tools.ResetCatalogue()
	t.Cleanup(tools.ResetCatalogue)

	single, multiOnly := registerAll()
	registered := map[string]bool{}
	for _, tool := range append(append([]mcp.Tool(nil), single...), multiOnly...) {
		registered[tool.Name] = true
	}

	for name, reason := range e2eSkips {
		if !registered[name] {
			t.Errorf("e2eSkips names %q, which is not a registered tool", name)
		}
		if len(reason) < 40 || !strings.HasSuffix(reason, ".") {
			t.Errorf("e2eSkips[%q] must be a complete sentence explaining the exclusion, got %q", name, reason)
		}
	}
}
