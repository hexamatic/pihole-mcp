package tools

import (
	"strings"
	"testing"
)

func TestActionGravity_Success(t *testing.T) {
	rec := piholeRawHandler(
		nil,
		map[string]string{
			"/action/gravity": "Downloading blocklist...\nDone.\n",
		},
	)
	c := newTestClient(t, rec)

	text := callTool(t, actionGravityHandler, c, nil)
	if !strings.Contains(text, "Gravity update complete") {
		t.Errorf("expected gravity complete message, got: %s", text)
	}
	if !strings.Contains(text, "```") {
		t.Errorf("expected code block wrapping, got: %s", text)
	}
	if !strings.Contains(text, "Downloading blocklist") {
		t.Errorf("expected raw output in code block, got: %s", text)
	}

	// The four action endpoints all answer on path alone in the fake, so every
	// assertion above passes for a GET as well. Against a real Pi-hole only the
	// POST rebuilds gravity; a GET would 404 and this tool would report a
	// failure it never had to have.
	rec.Only(t, "POST", "/action/gravity").AssertNoQueryString(t)
}

func TestActionRestartDNS_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/action/restartdns": map[string]any{"success": true},
	})
	c := newTestClient(t, rec)

	text := callTool(t, actionRestartDNSHandler, c, nil)
	if !strings.Contains(text, "DNS restarted") {
		t.Errorf("expected 'DNS restarted' message, got: %s", text)
	}

	// Restarting DNS interrupts resolution for every client on the network, so
	// this must be exactly one POST to the restart endpoint: a duplicate is a
	// second outage the reply would never mention.
	rec.Only(t, "POST", "/action/restartdns").AssertNoQueryString(t)
	rec.AssertCount(t, "", "", 1)
}

func TestActionFlushLogs_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/action/flush/logs": map[string]any{"success": true},
	})
	c := newTestClient(t, rec)

	text := callTool(t, actionFlushLogsHandler, c, nil)
	if !strings.Contains(text, "Logs flushed") {
		t.Errorf("expected 'Logs flushed' message, got: %s", text)
	}

	// Flushing logs is irreversible, and the two flush endpoints differ only by
	// their last path segment. Nothing in the reply is derived from the
	// response, so a call that landed on the network table instead would still
	// report the logs as flushed.
	rec.Only(t, "POST", "/action/flush/logs").AssertNoQueryString(t)
}

func TestActionFlushNetwork_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/action/flush/network": map[string]any{"success": true},
	})
	c := newTestClient(t, rec)

	text := callTool(t, actionFlushNetworkHandler, c, nil)
	if !strings.Contains(text, "Network table flushed") {
		t.Errorf("expected 'Network table flushed' message, got: %s", text)
	}

	// The mirror of the log flush: same verb, sibling path, and a reply that
	// would read the same if the wrong one of the two were called.
	rec.Only(t, "POST", "/action/flush/network").AssertNoQueryString(t)
}
