package tools

import (
	"strings"
	"testing"
)

// testGroups is a /groups reply with Default, as every Pi-hole has, and two
// more.
func testGroups() map[string]any {
	return map[string]any{"groups": []any{
		map[string]any{"name": "Default", "id": 0, "enabled": true},
		map[string]any{"name": "Guests", "id": 1, "enabled": true},
		map[string]any{"name": "Kids", "id": 2, "enabled": true},
	}}
}

func TestClientsAdd_PutsTheClientInTheNamedGroups(t *testing.T) {
	// No tool could assign groups, though the client tools said the group
	// tools did. FTL takes the IDs as a groups array on the entry.
	rec := piholeHandler(map[string]any{"/groups": testGroups(), "POST /clients": map[string]any{"clients": []any{}}})
	callTool(t, clientsAddHandler, newTestClient(t, rec), map[string]any{"client": "10.0.0.5", "groups": "kids, Default"})
	rec.Only(t, "POST", "/clients").AssertField(t, "groups", []any{2.0, 0.0})
}

func TestGroupsArgument_UnknownOrEmptyIsRefused(t *testing.T) {
	rec := piholeHandler(map[string]any{"/groups": testGroups()})
	c := newTestClient(t, rec)
	text := callToolExpectError(t, domainsAddHandler, c, map[string]any{"type": "deny", "kind": "exact", "domain": "a.example", "groups": "Kids,Teens"})
	if !strings.Contains(text, "no group named Teens") || !strings.Contains(text, "Default, Guests, Kids") {
		t.Errorf("expected the missing group named and the real ones listed, got: %s", text)
	}
	// An entry in no group applies to no client.
	callToolExpectError(t, listsAddHandler, c, map[string]any{"type": "block", "address": "https://example.com/l.txt", "groups": " , "})
	rec.AssertNone(t, "POST", "")
}

func TestGroupsArgument_OmittedLeavesMembershipAlone(t *testing.T) {
	// FTL keeps an entry's groups when the body has no groups array.
	rec := piholeHandler(map[string]any{
		"GET /domains/deny/exact/a.example": map[string]any{"domains": []any{map[string]any{"domain": "a.example", "comment": "old", "enabled": true}}},
		"PUT /domains/deny/exact/a.example": map[string]any{"domains": []any{}},
	})
	callTool(t, domainsUpdateHandler, newTestClient(t, rec), map[string]any{"type": "deny", "kind": "exact", "domain": "a.example", "comment": "new"})
	rec.Only(t, "PUT", "/domains/deny/exact/a.example").AssertNoField(t, "groups")
	rec.AssertNone(t, "GET", "/groups")
}

func TestListsUpdate_ReplacesTheGroups(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/groups":                              testGroups(),
		"GET /lists/https://example.com/l.txt": map[string]any{"lists": []any{map[string]any{"address": "https://example.com/l.txt", "type": "block", "enabled": true}}},
		"PUT /lists/https://example.com/l.txt": map[string]any{"lists": []any{}},
	})
	callTool(t, listsUpdateHandler, newTestClient(t, rec), map[string]any{"type": "block", "address": "https://example.com/l.txt", "groups": "Guests"})
	rec.Only(t, "PUT", "/lists/https://example.com/l.txt").AssertField(t, "groups", []any{1.0})
}

func TestClientsList_ShowsGroupNames(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/clients": map[string]any{"clients": []any{map[string]any{"client": "10.0.0.5", "name": "laptop", "groups": []any{0, 2}}}},
		"/groups":  testGroups(),
	}))
	text := callTool(t, clientsListHandler, c, nil)
	if !strings.Contains(text, "[groups: Default, Kids]") {
		t.Errorf("expected group names, got: %s", text)
	}
}
