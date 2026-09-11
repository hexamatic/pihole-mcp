package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/server"
)

// dnsLogHandler wraps logHandler for use with callTool.
func dnsLogHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return logHandler(r, "/logs/dnsmasq")
}

// ftlLogHandler wraps logHandler for use with callTool.
func ftlLogHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return logHandler(r, "/logs/ftl")
}

// webserverLogHandler wraps logHandler for use with callTool.
func webserverLogHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return logHandler(r, "/logs/webserver")
}

// next_id is what makes these three tools incremental: it is appended to the
// path as ?nextID=, and the rendered output is byte-for-byte identical whether
// or not it reaches the wire. A dropped parameter makes every poll re-return
// the whole log instead of only the new lines, and nothing in the reply says
// so. The same shape as the restart flag on the config value tools.
//
// logs.go builds the query by string concatenation rather than net/url.Values,
// so this is also the assertion that guards the eventual move to a proper
// encoder.
func TestLogs_NextIDReachesTheWire(t *testing.T) {
	for _, tc := range []struct {
		name     string
		handler  func(*pihole.Registry) server.ToolHandlerFunc
		endpoint string
	}{
		{"dnsmasq", dnsLogHandler, "/logs/dnsmasq"},
		{"ftl", ftlLogHandler, "/logs/ftl"},
		{"webserver", webserverLogHandler, "/logs/webserver"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := map[string]any{"log": []any{}, "nextID": 150}

			t.Run("sent when supplied", func(t *testing.T) {
				rec := piholeHandler(map[string]any{tc.endpoint: reply})
				c := newTestClient(t, rec)

				callTool(t, tc.handler, c, map[string]any{"next_id": 42.0})

				req := rec.Only(t, "GET", tc.endpoint)
				req.AssertQuery(t, "nextID", "42")
				req.AssertQueryKeys(t, "nextID")
			})

			t.Run("absent when omitted", func(t *testing.T) {
				rec := piholeHandler(map[string]any{tc.endpoint: reply})
				c := newTestClient(t, rec)

				callTool(t, tc.handler, c, nil)

				req := rec.Only(t, "GET", tc.endpoint)
				// A polling parameter that appears uninvited would silently
				// hide every line before it.
				req.AssertNoQuery(t, "nextID")
				req.AssertNoQueryString(t)
			})
		})
	}
}

func TestLogsDNS_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/logs/dnsmasq": map[string]any{
			"log": []any{
				map[string]any{"timestamp": 1700000000, "message": "query[A] example.com from 192.168.1.10", "prio": "info"},
				map[string]any{"timestamp": 1700000001, "message": "reply example.com is 93.184.216.34", "prio": "info"},
			},
			"nextID": 42,
		},
	}))

	text := callTool(t, dnsLogHandler, c, nil)
	if !strings.Contains(text, "2 lines") {
		t.Errorf("expected '2 lines' header, got: %s", text)
	}
	if !strings.Contains(text, "query[A] example.com") {
		t.Errorf("expected first log message, got: %s", text)
	}
	if !strings.Contains(text, "Next ID: 42") {
		t.Errorf("expected next ID, got: %s", text)
	}
}

func TestLogsDNS_WithNextID(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/logs/dnsmasq": map[string]any{
			"log": []any{
				map[string]any{"timestamp": 1700000005, "message": "query[A] new.example.com from 192.168.1.10", "prio": "info"},
			},
			"nextID": 150,
		},
	}))

	text := callTool(t, dnsLogHandler, c, map[string]any{"next_id": 100.0})
	if !strings.Contains(text, "1 lines") {
		t.Errorf("expected '1 lines' header, got: %s", text)
	}
	if !strings.Contains(text, "new.example.com") {
		t.Errorf("expected log message, got: %s", text)
	}
	if !strings.Contains(text, "Next ID: 150") {
		t.Errorf("expected next ID, got: %s", text)
	}
}

func TestLogsDNS_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/logs/dnsmasq": map[string]any{
			"log":    []any{},
			"nextID": 99,
		},
	}))

	text := callTool(t, dnsLogHandler, c, nil)
	if !strings.Contains(text, "No log entries") {
		t.Errorf("expected empty log message, got: %s", text)
	}
	if !strings.Contains(text, "Next ID: 99") {
		t.Errorf("expected next ID in empty response, got: %s", text)
	}
}

func TestLogsDNS_TruncatedAt50(t *testing.T) {
	entries := make([]any, 60)
	for i := range entries {
		entries[i] = map[string]any{
			"timestamp": 1700000000 + i,
			"message":   fmt.Sprintf("log entry %d", i),
			"prio":      "info",
		}
	}

	c := newTestClient(t, piholeHandler(map[string]any{
		"/logs/dnsmasq": map[string]any{
			"log":    entries,
			"nextID": 200,
		},
	}))

	text := callTool(t, dnsLogHandler, c, nil)
	if !strings.Contains(text, "60 lines") {
		t.Errorf("expected '60 lines' in header, got: %s", text)
	}
	if !strings.Contains(text, "showing 50") {
		t.Errorf("expected 'showing 50' in header, got: %s", text)
	}
	// Entry 49 should be present (0-indexed), entry 50 should not.
	if !strings.Contains(text, "log entry 49") {
		t.Errorf("expected last shown entry (49), got: %s", text)
	}
	if strings.Contains(text, "log entry 50") {
		t.Errorf("entry 50 should be truncated, got: %s", text)
	}
}

func TestLogsFTL_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/logs/ftl": map[string]any{
			"log": []any{
				map[string]any{"timestamp": 1700000000, "message": "FTL started", "prio": "info"},
			},
			"nextID": 10,
		},
	}))

	text := callTool(t, ftlLogHandler, c, nil)
	if !strings.Contains(text, "1 lines") {
		t.Errorf("expected '1 lines' header, got: %s", text)
	}
	if !strings.Contains(text, "FTL started") {
		t.Errorf("expected FTL log message, got: %s", text)
	}
}
