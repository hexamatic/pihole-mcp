package tools

import (
	"strings"
	"testing"
)

func TestAuthSessions_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/auth/sessions": map[string]any{
			"sessions": []any{
				map[string]any{"id": 1, "remote_addr": "192.168.1.10", "user_agent": "curl/8.0", "valid_until": 1700003600.0, "this": false},
				map[string]any{"id": 2, "remote_addr": "192.168.1.20", "user_agent": "Mozilla/5.0", "valid_until": 1700007200.0, "this": false},
			},
		},
	}))

	text := callTool(t, authSessionsHandler, c, nil)
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected first session address, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.20") {
		t.Errorf("expected second session address, got: %s", text)
	}
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
	c := newTestClient(t, piholeHandler(map[string]any{
		"/auth/session/5": nil,
	}))

	text := callTool(t, authRevokeSessionHandler, c, map[string]any{
		"id": 5.0,
	})
	if !strings.Contains(text, "revoked") {
		t.Errorf("expected 'revoked' message, got: %s", text)
	}
	if !strings.Contains(text, "5") {
		t.Errorf("expected session ID in response, got: %s", text)
	}
}
