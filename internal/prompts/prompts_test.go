package prompts

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type promptHandler func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error)

func request(args map[string]string) mcp.GetPromptRequest {
	var req mcp.GetPromptRequest
	req.Params.Arguments = args
	return req
}

// requireSingleUserMessage asserts the shared shape of every prompt result
// and returns its text.
func requireSingleUserMessage(t *testing.T, res *mcp.GetPromptResult) string {
	t.Helper()
	if res.Description == "" {
		t.Error("empty description")
	}
	if len(res.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(res.Messages))
	}
	m := res.Messages[0]
	if m.Role != mcp.RoleUser {
		t.Errorf("role = %q, want user", m.Role)
	}
	text, ok := m.Content.(mcp.TextContent)
	if !ok {
		t.Fatalf("content type %T, want TextContent", m.Content)
	}
	if strings.TrimSpace(text.Text) == "" {
		t.Error("empty prompt text")
	}
	return text.Text
}

func TestAllHandlers_ShapeAndContent(t *testing.T) {
	tests := []struct {
		name     string
		handler  promptHandler
		wantText string // substring every invocation must contain
	}{
		{"diagnose_slow_dns", diagnoseSlowDNSHandler, "pihole_stats_upstreams"},
		{"investigate_domain", investigateDomainHandler, "pihole_search_domains"},
		{"review_top_blocked", reviewTopBlockedHandler, "false positive"},
		{"audit_network", auditNetworkHandler, "pihole_network_devices"},
		{"optimise_blocklists", optimiseBlocklistsHandler, "pihole_lists_list"},
		{"daily_report", dailyReportHandler, "pihole_stats_summary"},
		{"security_audit", securityAuditHandler, "pihole_auth_sessions"},
		{"weekly_trends", weeklyTrendsHandler, "pihole_stats_database"},
		{"upstream_health", upstreamHealthHandler, "pihole_stats_upstreams"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tt.handler(context.Background(), request(nil))
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			text := requireSingleUserMessage(t, res)
			if !strings.Contains(text, tt.wantText) {
				t.Errorf("prompt text missing %q", tt.wantText)
			}
		})
	}
}

func TestInvestigateDomain_ArgumentSubstitution(t *testing.T) {
	res, err := investigateDomainHandler(context.Background(),
		request(map[string]string{"domain": "ads.example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	text := requireSingleUserMessage(t, res)
	if !strings.Contains(text, "**ads.example.com**") {
		t.Errorf("domain not substituted into prompt text")
	}
	if !strings.Contains(res.Description, "ads.example.com") {
		t.Errorf("domain not substituted into description: %q", res.Description)
	}

	// Default placeholder when the argument is absent.
	res, err = investigateDomainHandler(context.Background(), request(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(requireSingleUserMessage(t, res), "<domain>") {
		t.Error("missing <domain> placeholder without argument")
	}
}

func TestReviewTopBlocked_CountDefault(t *testing.T) {
	res, err := reviewTopBlockedHandler(context.Background(), request(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(requireSingleUserMessage(t, res), "20") {
		t.Error("default count 20 not in prompt text")
	}

	res, err = reviewTopBlockedHandler(context.Background(),
		request(map[string]string{"count": "50"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(requireSingleUserMessage(t, res), "50") {
		t.Error("count argument not substituted")
	}
}

func TestWeeklyTrends_WeeksBackDefault(t *testing.T) {
	res, err := weeklyTrendsHandler(context.Background(), request(nil))
	if err != nil {
		t.Fatal(err)
	}
	requireSingleUserMessage(t, res)

	res, err = weeklyTrendsHandler(context.Background(),
		request(map[string]string{"weeks_back": "4"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(requireSingleUserMessage(t, res), "4") {
		t.Error("weeks_back argument not substituted")
	}
}

func TestRegisterAll(t *testing.T) {
	s := server.NewMCPServer("test", "0.0.0", server.WithPromptCapabilities(false))
	RegisterAll(s) // must not panic; registration is the only observable effect
}

// TestPromptsReferenceOnlyReadOnlyTools pins a fact that is true today and
// would be invisible if it stopped being true.
//
// Prompts are not filtered in read-only mode, and the reason is simply that
// none of them instructs a write: every pihole_* name across all nine prompt
// handlers is a read-only tool. That makes filtering unnecessary rather than
// merely inconvenient. The day someone writes a prompt that says "then call
// pihole_domains_add", a read-only deployment would start steering models into
// a rejected call, so this fails and forces the decision then, when there is a
// real case to design against. mcp-go's server.WithPromptFilter is the seam if
// it is ever needed.
func TestPromptsReferenceOnlyReadOnlyTools(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.0")
	tools.ResetCatalogue()
	tools.RegisterAll(srv, pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "primary", URL: "http://127.0.0.1:1", Password: "x"},
	}))

	readOnly := map[string]bool{}
	for _, tool := range tools.Catalogue() {
		readOnly[tool.Name] = tools.IsReadOnly(tool)
	}
	if len(readOnly) == 0 {
		t.Fatal("no tools registered")
	}

	names := regexp.MustCompile(`pihole_[a-z0-9_]+`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		//nolint:gosec // G304: name comes from reading this package's own directory, never from input
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		for _, tool := range names.FindAllString(string(source), -1) {
			ro, known := readOnly[tool]
			if !known {
				t.Errorf("%s names %s, which is not a registered tool", name, tool)
				continue
			}
			checked++
			if !ro {
				t.Errorf("%s instructs the model to call %s, which read-only mode rejects. "+
					"Either keep prompts read-only, or filter them with server.WithPromptFilter.", name, tool)
			}
		}
	}
	if checked == 0 {
		t.Error("no tool names were found in the prompt sources; this test is no longer checking anything")
	}
}
