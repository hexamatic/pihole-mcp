package tools

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestRequireStrings pins the behaviour twelve call sites across
// domains/groups/clients/lists now depend on: every key present succeeds in
// order, and the first missing key is named in the error rather than being
// silently discarded and the request sent with an empty string. The bare
// `x, _ := req.RequireString(k)` this replaced could not tell a caller which
// of several required parameters they forgot.
func TestRequireStrings(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"type": "deny", "kind": "exact"}

	vals, err := requireStrings(req, "type", "kind")
	if err != nil {
		t.Fatalf("unexpected error with both parameters present: %v", err)
	}
	if vals[0] != "deny" || vals[1] != "exact" {
		t.Errorf("expected [deny exact], got %v", vals)
	}

	_, err = requireStrings(req, "type", "kind", "domain")
	if err == nil {
		t.Fatal("expected an error for the missing 'domain' parameter")
	}
	if !strings.Contains(err.Error(), "'domain'") {
		t.Errorf("expected the error to name 'domain', got: %v", err)
	}

	_, err = requireStrings(req, "domain", "type")
	if err == nil || !strings.Contains(err.Error(), "'domain'") {
		t.Errorf("expected the first missing key ('domain') to be named, got: %v", err)
	}
}
