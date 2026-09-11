package toolsets

import (
	"strings"
	"testing"
)

// TestToolsetTableIsWellFormed locks the invariants a published contract
// depends on: names are unique, no token is claimed by two toolsets, and every
// token has a display heading. A token owned twice would make Of's answer
// depend on map iteration order.
func TestToolsetTableIsWellFormed(t *testing.T) {
	seenName := map[string]bool{}
	seenToken := map[string]string{}

	for _, ts := range table {
		if ts.Name == "" {
			t.Error("toolset with an empty name")
		}
		if ts.Name == All {
			t.Errorf("%q collides with the reserved 'all' selector", ts.Name)
		}
		if ts.Name != strings.ToLower(ts.Name) {
			t.Errorf("%q is not lowercase; the published names are matched case-sensitively", ts.Name)
		}
		if seenName[ts.Name] {
			t.Errorf("duplicate toolset name %q", ts.Name)
		}
		seenName[ts.Name] = true

		if ts.Summary == "" {
			t.Errorf("%s: empty summary", ts.Name)
		}
		if len(ts.Tokens) == 0 {
			t.Errorf("%s: owns no tokens, so no tool can ever belong to it", ts.Name)
		}
		for _, tok := range ts.Tokens {
			if owner, ok := seenToken[tok]; ok {
				t.Errorf("token %q claimed by both %q and %q", tok, owner, ts.Name)
			}
			seenToken[tok] = ts.Name
			if _, ok := Heading(tok); !ok {
				t.Errorf("token %q has no display heading", tok)
			}
		}
	}

	for tok := range headings {
		if _, ok := seenToken[tok]; !ok {
			t.Errorf("heading for token %q belongs to no toolset", tok)
		}
	}
}

func TestToken(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"pihole_stats_summary", "stats"},
		{"pihole_padd", "padd"},
		{"pihole_local_dns_list", "local"},
		{"pihole", ""},
		{"", ""},
	} {
		if got := Token(tc.in); got != tc.want {
			t.Errorf("Token(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOf(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"pihole_search_domains", "domains", true},
		{"pihole_local_cname_add", "dns", true},
		{"pihole_auth_sessions", "sessions", true},
		{"pihole_action_flush_logs", "actions", true},
		{"pihole_unknown_thing", "", false},
	} {
		got, ok := Of(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Of(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Validate([]string{"dns", "stats", All}); err != nil {
		t.Errorf("unexpected error for valid names: %v", err)
	}

	err := Validate([]string{"dns", "doamins"})
	if err == nil {
		t.Fatal("expected an error for an unknown toolset")
	}
	// The message is usually all a user gets: a stdio server that exits at
	// startup often shows nothing but "disconnected", so the bad value and
	// every valid name have to be in the one line.
	if !strings.Contains(err.Error(), `"doamins"`) {
		t.Errorf("error does not quote the offending value: %v", err)
	}
	for _, name := range Names() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not list the valid name %q: %v", name, err)
		}
	}
}

func TestResolve(t *testing.T) {
	if got := Resolve(nil); got != nil {
		t.Errorf("Resolve(nil) = %v, want nil (unrestricted)", got)
	}
	if got := Resolve([]string{}); got != nil {
		t.Errorf("Resolve(empty) = %v, want nil (unrestricted)", got)
	}
	// "all" is a synonym for unset, including when mixed with other names.
	if got := Resolve([]string{"dns", All}); got != nil {
		t.Errorf("Resolve with 'all' = %v, want nil (unrestricted)", got)
	}
	got := Resolve([]string{"dns", "stats"})
	if len(got) != 2 || !got["dns"] || !got["stats"] {
		t.Errorf("Resolve([dns stats]) = %v", got)
	}
}
