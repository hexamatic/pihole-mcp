package server

import (
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/hexamatic/pihole-mcp/internal/toolsets"
)

func TestInstructions_NameOnlyToolsTheDeploymentHas(t *testing.T) {
	// The instructions named tools whatever PIHOLE_TOOLSETS said, so a DNS-only
	// deployment was told in every conversation to start with pihole_padd.
	selected := toolsets.Resolve([]string{"dns"})
	text := instructions(false, selected)
	for _, name := range toolNamesIn(text) {
		if set, ok := toolsets.Of(name); !ok || !selected[set] {
			t.Errorf("instructions for the dns toolset name %s, from toolset %q", name, set)
		}
	}
	if !strings.Contains(text, "Pi-hole v6 DNS management server.") {
		t.Errorf("sentences that name no tool should stay, got: %s", text)
	}
	if full := instructions(false, nil); !strings.Contains(full, "pihole_padd") {
		t.Errorf("the default instructions lost pihole_padd: %s", full)
	}
}

func TestPrompts_OfferedOnlyWhenTheirToolsAre(t *testing.T) {
	all := newScopedDispatchServer(t, nil, newDispatchFake(t))
	if n := len(all.ListPrompts()); n != 9 {
		t.Errorf("the default deployment offers %d prompts, want 9", n)
	}

	// Every prompt names tools outside the DNS toolset.
	dnsOnly := newScopedDispatchServer(t, []Option{WithToolsets([]string{"dns"})}, newDispatchFake(t))
	if n := len(dnsOnly.ListPrompts()); n != 0 {
		t.Errorf("a DNS-only deployment offers %d prompts, none of which it can follow", n)
	}

	// investigate_domain needs cross-list search (in the domains toolset),
	// the query log, and local DNS (in the dns toolset).
	some := newScopedDispatchServer(t, []Option{WithToolsets([]string{"domains", "queries", "dns"})}, newDispatchFake(t))
	prompts := some.ListPrompts()
	if _, ok := prompts["investigate_domain"]; !ok || len(prompts) != 1 {
		t.Errorf("offered prompts = %v, want investigate_domain alone", keys(prompts))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

var _ = piholefake.New // the dispatch helpers build on the fake
