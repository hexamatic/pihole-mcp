package tools

import (
	"strings"
	"testing"
)

func TestPADD_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/padd": loadFixture(t, "padd"),
	}))

	text := callTool(t, paddHandler, c, nil)

	for _, want := range []string{"enabled", "48,213", "github.com", "ads.example.net", "v6.6.2"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in dashboard output, got:\n%s", want, text)
		}
	}
	// Interface detail should NOT appear at normal detail.
	if strings.Contains(text, "IPv4") {
		t.Errorf("did not expect interface detail at normal detail, got:\n%s", text)
	}
}

func TestPADD_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/padd": loadFixture(t, "padd"),
	}))

	text := callTool(t, paddHandler, c, map[string]any{"detail": "minimal"})

	if !strings.Contains(text, "Blocking: enabled") {
		t.Errorf("expected minimal one-liner, got: %s", text)
	}
	if strings.Contains(text, "\n**") {
		t.Errorf("minimal output should be a single line, got: %s", text)
	}
}

func TestPADD_Full(t *testing.T) {
	// At full detail the handler requests /padd?full=true; piholeHandler routes
	// by path only, so the "/padd" route serves regardless of query string.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/padd": loadFixture(t, "padd"),
	}))

	text := callTool(t, paddHandler, c, map[string]any{"detail": "full"})

	for _, want := range []string{"IPv4", "192.168.1.10", "Raspberry Pi"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in full dashboard output, got:\n%s", want, text)
		}
	}
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
