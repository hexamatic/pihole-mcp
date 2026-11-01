package tools

import (
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/format"
)

func TestPADD_Normal(t *testing.T) {
	eachFixture(t, "padd", func(t *testing.T, _ string, fixture any) {
		c := newTestClient(t, piholeHandler(map[string]any{"/padd": fixture}))
		text := callTool(t, paddHandler, c, nil)

		for _, want := range []string{
			"**Blocking:** " + fixtureString(t, fixture, "blocking"),
			"**Queries:** " + format.Number(fixtureInt(t, fixture, "queries.total")) + " total, " +
				format.Number(fixtureInt(t, fixture, "queries.blocked")) + " blocked",
			"**Top domain:** " + fixtureString(t, fixture, "top_domain"),
			"**Top blocked:** " + fixtureString(t, fixture, "top_blocked"),
			"**Top client:** " + fixtureString(t, fixture, "top_client"),
			"**Gravity:** " + format.Number(fixtureInt(t, fixture, "gravity_size")) + " domains",
			"FTL " + fixtureString(t, fixture, "version.ftl.local.version"),
			"**Host:** " + fixtureString(t, fixture, "node_name"),
		} {
			if !strings.Contains(text, want) {
				t.Errorf("expected %q in dashboard output, got:\n%s", want, text)
			}
		}
		// Interface detail should NOT appear at normal detail.
		if strings.Contains(text, "IPv4") {
			t.Errorf("did not expect interface detail at normal detail, got:\n%s", text)
		}
	})
}

func TestPADD_Minimal(t *testing.T) {
	eachFixture(t, "padd", func(t *testing.T, _ string, fixture any) {
		c := newTestClient(t, piholeHandler(map[string]any{"/padd": fixture}))
		text := callTool(t, paddHandler, c, map[string]any{"detail": "minimal"})

		want := "Blocking: " + fixtureString(t, fixture, "blocking") +
			" | Queries: " + format.Number(fixtureInt(t, fixture, "queries.total"))
		if !strings.HasPrefix(text, want) {
			t.Errorf("expected the one-liner to start %q, got: %s", want, text)
		}
		if strings.Contains(text, "\n") {
			t.Errorf("minimal output should be a single line, got: %s", text)
		}
	})
}

func TestPADD_Full(t *testing.T) {
	// At full detail the handler requests /padd?full=true; piholeHandler routes
	// by path only, so the "/padd" route serves regardless of query string.
	// The plain capture carries the interface block too.
	eachFixture(t, "padd", func(t *testing.T, _ string, fixture any) {
		c := newTestClient(t, piholeHandler(map[string]any{"/padd": fixture}))
		text := callTool(t, paddHandler, c, map[string]any{"detail": "full"})

		want := "**IPv4 (" + fixtureString(t, fixture, "iface.v4.name") + "):** " + fixtureString(t, fixture, "iface.v4.addr")
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in full dashboard output, got:\n%s", want, text)
		}
		if gw := fixtureString(t, fixture, "iface.v4.gw_addr"); !strings.Contains(text, gw) {
			t.Errorf("expected the gateway %s in full dashboard output, got:\n%s", gw, text)
		}
	})
}

// TestPADD_RealFixture locks the response shape: every field the handler reads
// must decode from the canonical fixture without error.
func TestPADD_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/padd": loadFixture(t, "padd"),
	}))
	if text := callTool(t, paddHandler, c, nil); text == "" {
		t.Fatal("expected non-empty response from real fixture")
	}
}
