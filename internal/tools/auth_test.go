package tools

import (
	"strings"
	"testing"
)

func TestAuthSessions_Normal(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/auth/sessions": map[string]any{
			"sessions": []any{
				map[string]any{"id": 1, "remote_addr": "192.168.1.10", "user_agent": "curl/8.0", "valid_until": 1700003600.0, "this": false},
				map[string]any{"id": 2, "remote_addr": "192.168.1.20", "user_agent": "Mozilla/5.0", "valid_until": 1700007200.0, "this": false},
			},
		},
	})
	c := newTestClient(t, h)

	text := callTool(t, authSessionsHandler, c, nil)
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected first session address, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.20") {
		t.Errorf("expected second session address, got: %s", text)
	}

	// Listing sessions must be a read. The route table answers on path alone,
	// so a method slip here would have gone unnoticed, and on a real Pi-hole
	// anything but GET on the session endpoints tears sessions down rather
	// than reporting them: a security-audit tool that logs everybody out.
	h.Only(t, "GET", "/auth/sessions")
}

func TestAuthSessions_CurrentMarker(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/auth/sessions": map[string]any{
			"sessions": []any{
				map[string]any{"id": 1, "remote_addr": "192.168.1.10", "user_agent": "curl/8.0", "valid_until": 1700003600.0, "this": true},
			},
		},
	}))

	text := callTool(t, authSessionsHandler, c, nil)
	if !strings.Contains(text, "(current)") {
		t.Errorf("expected '(current)' marker for current session, got: %s", text)
	}
}

func TestAuthSessions_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/auth/sessions": map[string]any{"sessions": []any{}},
	}))

	text := callTool(t, authSessionsHandler, c, nil)
	if !strings.Contains(text, "No active sessions") {
		t.Errorf("expected empty sessions message, got: %s", text)
	}
}

func TestAuthSessions_RealFixture(t *testing.T) {
	// Real captured response — protects against shape drift.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/auth/sessions": loadFixture(t, "auth_sessions"),
	}))

	text := callTool(t, authSessionsHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty sessions output from real fixture")
	}
	if !strings.Contains(strings.ToLower(text), "session") {
		t.Errorf("expected the word 'session' (any case) in fixture output, got: %s", text)
	}
}

func TestAuthRevokeSession_Success(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/auth/session/5": nil,
	})
	c := newTestClient(t, h)

	text := callTool(t, authRevokeSessionHandler, c, map[string]any{
		"id": 5.0,
	})
	if !strings.Contains(text, "revoked") {
		t.Errorf("expected 'revoked' message, got: %s", text)
	}
	if !strings.Contains(text, "5") {
		t.Errorf("expected session ID in response, got: %s", text)
	}
	// The confirmation text is built from the argument, not from the reply, so
	// it says "Session 5 revoked" whatever went onto the wire. Two things can
	// only be seen here: that the request was a DELETE, and that it addressed
	// the singular /auth/session/{id} rather than the plural listing endpoint
	// next door, where a DELETE would take out every session at once.
	req := h.Only(t, "DELETE", "")
	req.AssertRawPath(t, "/auth/session/5")
	req.AssertNoBody(t)
	req.AssertNoQueryString(t)
}

func TestAuthRevokeSession_MissingIDNeverReachesTheAPI(t *testing.T) {
	h := piholeHandler(map[string]any{})
	c := newTestClient(t, h)

	text := callToolExpectError(t, authRevokeSessionHandler, c, nil)
	if !strings.Contains(text, "Parameter 'id' is required") {
		t.Errorf("expected required-id error, got: %s", text)
	}

	// A missing id defaults to 0 before validation. Should that guard ever be
	// relaxed, the request that escapes is DELETE /auth/session/0, and this is
	// the only place that would notice: the tool still returns an error either
	// way, because the fake has no such route.
	h.AssertNone(t, "", "")
}
