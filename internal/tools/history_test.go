package tools

import (
	"strings"
	"testing"
)

func TestHistoryGraph_InMemory(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history": map[string]any{
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "total": 100, "cached": 30, "blocked": 20, "forwarded": 50},
				map[string]any{"timestamp": 1700000600.0, "total": 120, "cached": 35, "blocked": 25, "forwarded": 60},
			},
		},
	}))

	text := callTool(t, historyGraphHandler, c, nil)
	if !strings.Contains(text, "2 data points") {
		t.Errorf("expected '2 data points', got: %s", text)
	}
	if !strings.Contains(text, "220") {
		t.Errorf("expected total queries (220), got: %s", text)
	}
}

func TestHistoryGraph_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history": map[string]any{
			"history": []any{},
		},
	}))

	text := callTool(t, historyGraphHandler, c, nil)
	if text != "No history data available." {
		t.Errorf("expected empty history message, got: %s", text)
	}
}

func TestHistoryClients_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": "desktop", "total": 500},
				"192.168.1.20": map[string]any{"name": nil, "total": 200},
			},
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "data": map[string]any{"192.168.1.10": 50, "192.168.1.20": 20}},
			},
		},
	}))

	text := callTool(t, historyClientsHandler, c, nil)
	if !strings.Contains(text, "2 clients") {
		t.Errorf("expected '2 clients', got: %s", text)
	}
	if !strings.Contains(text, "desktop") {
		t.Errorf("expected named client 'desktop', got: %s", text)
	}
	if !strings.Contains(text, "500") {
		t.Errorf("expected query count for desktop, got: %s", text)
	}
}

func TestHistoryClients_NilName(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.30": map[string]any{"name": nil, "total": 75},
			},
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "data": map[string]any{"192.168.1.30": 30}},
			},
		},
	}))

	text := callTool(t, historyClientsHandler, c, nil)
	if !strings.Contains(text, "1 clients") {
		t.Errorf("expected '1 clients', got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.30") {
		t.Errorf("expected client IP, got: %s", text)
	}
	if !strings.Contains(text, "75") {
		t.Errorf("expected query count, got: %s", text)
	}
}

func TestHistoryDatabase_RangeProvided(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/database": map[string]any{
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "total": 100, "cached": 30, "blocked": 20, "forwarded": 50},
				map[string]any{"timestamp": 1700086400.0, "total": 200, "cached": 60, "blocked": 40, "forwarded": 100},
			},
		},
	}))

	text := callTool(t, historyDatabaseHandler, c, map[string]any{
		"from":  1700000000.0,
		"until": 1700172800.0,
	})
	if !strings.Contains(text, "2 data points") {
		t.Errorf("expected '2 data points', got: %s", text)
	}
	if !strings.Contains(text, "300") {
		t.Errorf("expected total queries (300), got: %s", text)
	}
}

func TestHistoryDatabase_DefaultRange(t *testing.T) {
	// Without explicit from/until, the handler should still issue both query
	// params via getTimeRange and return the empty-history message.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/database": map[string]any{
			"history": []any{},
		},
	}))

	text := callTool(t, historyDatabaseHandler, c, nil)
	if text != "No history data available." {
		t.Errorf("expected empty history message, got: %s", text)
	}
}

func TestHistoryDatabaseClients_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/database/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": "desktop", "total": 12345},
				"0.0.0.0":      map[string]any{"name": "other clients", "total": 678},
			},
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "data": map[string]any{"192.168.1.10": 1234}},
			},
		},
	}))

	text := callTool(t, historyDatabaseClientsHandler, c, map[string]any{
		"from":  1700000000.0,
		"until": 1700604800.0,
	})
	if !strings.Contains(text, "2 clients") {
		t.Errorf("expected '2 clients', got: %s", text)
	}
	if !strings.Contains(text, "desktop") {
		t.Errorf("expected named client 'desktop', got: %s", text)
	}
	if !strings.Contains(text, "other clients") {
		t.Errorf("expected 'other clients' bucket, got: %s", text)
	}
}
