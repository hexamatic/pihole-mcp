package prompts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
)

func domainsTestProvider(t *testing.T) *CompletionProvider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"valid": true, "sid": "s"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"domains": []any{
				map[string]any{"domain": "ads.example.com"},
				map[string]any{"domain": "tracker.example.net"},
				map[string]any{"domain": "analytics.example.com"},
			},
		})
	}))
	t.Cleanup(srv.Close)
	reg := pihole.NewRegistry([]pihole.InstanceConfig{{Name: "primary", URL: srv.URL, Password: "test"}})
	return NewCompletionProvider(reg)
}

func TestCompletion_FiltersByValue(t *testing.T) {
	p := domainsTestProvider(t)
	got, err := p.CompletePromptArgument(context.Background(), "investigate_domain",
		mcp.CompleteArgument{Name: "domain", Value: "example.com"}, mcp.CompleteContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Values) != 2 {
		t.Fatalf("expected 2 matches for 'example.com', got %v", got.Values)
	}
	for _, v := range got.Values {
		if !strings.Contains(v, "example.com") {
			t.Errorf("unexpected completion value %q", v)
		}
	}
}

func TestCompletion_IgnoresOtherPromptsAndArgs(t *testing.T) {
	p := domainsTestProvider(t)

	got, _ := p.CompletePromptArgument(context.Background(), "other_prompt",
		mcp.CompleteArgument{Name: "domain", Value: ""}, mcp.CompleteContext{})
	if len(got.Values) != 0 {
		t.Errorf("expected no completions for unrelated prompt, got %v", got.Values)
	}

	got, _ = p.CompletePromptArgument(context.Background(), "investigate_domain",
		mcp.CompleteArgument{Name: "count", Value: ""}, mcp.CompleteContext{})
	if len(got.Values) != 0 {
		t.Errorf("expected no completions for unrelated argument, got %v", got.Values)
	}
}

func TestCompletion_EmptyValueReturnsAll(t *testing.T) {
	p := domainsTestProvider(t)
	got, _ := p.CompletePromptArgument(context.Background(), "investigate_domain",
		mcp.CompleteArgument{Name: "domain", Value: ""}, mcp.CompleteContext{})
	if len(got.Values) != 3 {
		t.Errorf("expected all 3 domains for empty value, got %v", got.Values)
	}
}
