package tools

import (
	"strings"
	"testing"
)

func TestDNSGetBlocking_Enabled(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/dns/blocking": map[string]any{"blocking": "enabled"},
	}))

	text := callTool(t, dnsGetBlockingHandler, c, nil)
	if !strings.Contains(text, "enabled") {
		t.Errorf("expected 'enabled' in response, got: %s", text)
	}
}

func TestDNSGetBlocking_WithTimer(t *testing.T) {
	timer := 45.0
	c := newTestClient(t, piholeHandler(map[string]any{
		"/dns/blocking": map[string]any{"blocking": "disabled", "timer": timer},
	}))

	text := callTool(t, dnsGetBlockingHandler, c, nil)
	if !strings.Contains(text, "disabled") {
		t.Errorf("expected 'disabled' in response, got: %s", text)
	}
	if !strings.Contains(text, "45") {
		t.Errorf("expected timer '45' in response, got: %s", text)
	}
}

func TestDNSSetBlocking(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/dns/blocking": map[string]any{"blocking": "disabled", "timer": 60.0},
	})
	c := newTestClient(t, rec)

	text := callTool(t, dnsSetBlockingHandler, c, map[string]any{
		"blocking": false,
		"timer":    60.0,
	})
	if !strings.Contains(text, "disabled") {
		t.Errorf("expected 'disabled' in response, got: %s", text)
	}

	// The reply is canned, so the text above stays green whatever went out.
	// This tool is the one that switches blocking off for the whole network,
	// and the fake answers a GET of the same path identically, so the verb and
	// the body are the only evidence the change was actually requested.
	req := rec.Only(t, "POST", "/dns/blocking")
	req.AssertNoQueryString(t)
	req.AssertBodyKeys(t, "blocking", "timer")
	req.AssertField(t, "blocking", false)
	// A timer that went out as a string would be rejected rather than applied,
	// so the JSON type matters as much as the number.
	req.AssertField(t, "timer", 60)
}

// Omitting the timer must leave the key off the wire entirely. A permanent
// change and one that reverts are different requests, and FTL tells them apart
// by the key's presence: a timer of zero or null would schedule an immediate
// revert of the change the caller asked to keep.
func TestDNSSetBlocking_OmittedTimerIsNotSent(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/dns/blocking": map[string]any{"blocking": "enabled"},
	})
	c := newTestClient(t, rec)

	callTool(t, dnsSetBlockingHandler, c, map[string]any{"blocking": true})

	req := rec.Only(t, "POST", "/dns/blocking")
	req.AssertNoQueryString(t)
	req.AssertBodyKeys(t, "blocking")
	req.AssertField(t, "blocking", true)
}
