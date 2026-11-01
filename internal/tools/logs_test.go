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
	if !strings.Contains(text, "No log lines") {
		t.Errorf("expected empty log message, got: %s", text)
	}
	if !strings.Contains(text, "Next ID: 99") {
		t.Errorf("expected next ID in empty response, got: %s", text)
	}
}

func TestLogsDNS_ShowsTheNewest50(t *testing.T) {
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
	if !strings.Contains(text, "showing the newest 50") {
		t.Errorf("expected 'showing the newest 50' in header, got: %s", text)
	}
	// The newest 50 are entries 10 to 59; the first ten are left out.
	if !strings.Contains(text, "log entry 59\n") || !strings.Contains(text, "log entry 10\n") {
		t.Errorf("expected entries 10 to 59, got: %s", text)
	}
	if strings.Contains(text, "log entry 9\n") {
		t.Errorf("entry 9 is older than the newest 50 and should be left out, got: %s", text)
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

func TestLogs_ShowTheNewestLines(t *testing.T) {
	// FTL returns lines oldest first. The tool showed the first 50 and gave a
	// cursor past the last, so the newest lines were never seen and polling
	// from the cursor skipped the rest for good.
	eachFixture(t, "logs_dnsmasq", func(t *testing.T, _ string, fixture any) {
		entries := fixtureValue(t, fixture, "log").([]any)
		if len(entries) <= 10 {
			t.Fatalf("the capture holds %d lines; it needs more than 10 for this test", len(entries))
		}
		c := newTestClient(t, piholeHandler(map[string]any{"/logs/dnsmasq": fixture}))
		text := callTool(t, dnsLogHandler, c, map[string]any{"lines": 10.0})

		newest := entries[len(entries)-1].(map[string]any)["message"].(string)
		older := entries[len(entries)-11].(map[string]any)["message"].(string)
		if !strings.Contains(text, newest) {
			t.Errorf("expected the newest line %q, got:\n%s", newest, text)
		}
		if strings.Contains(text, "- "+older) || strings.Count(text, "\n- ") != 10 {
			t.Errorf("expected exactly the newest 10 lines, got:\n%s", text)
		}
		if want := fmt.Sprintf("showing the newest 10. Pass lines=%d", len(entries)); !strings.Contains(text, want) {
			t.Errorf("expected %q, got:\n%s", want, text)
		}
		if want := fmt.Sprintf("Next ID: %d", fixtureInt(t, fixture, "nextID")); !strings.Contains(text, want) {
			t.Errorf("expected %q, got:\n%s", want, text)
		}
	})
}

func TestLogsFTL_LevelKeepsTheSevereLines(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/logs/ftl": map[string]any{
			"log": []any{
				map[string]any{"timestamp": 1700000000, "message": "Imported 3 queries", "prio": "INFO"},
				map[string]any{"timestamp": 1700000001, "message": "Rate-limiting 10.0.0.5", "prio": "WARNING"},
				map[string]any{"timestamp": 1700000002, "message": "Cannot open database", "prio": "ERROR"},
				map[string]any{"timestamp": 1700000003, "message": "dnsmasq line", "prio": nil},
			},
			"nextID": 4,
		},
	}))

	text := callTool(t, ftlLogHandler, c, map[string]any{"level": "warning"})
	if !strings.Contains(text, "WARNING Rate-limiting") || !strings.Contains(text, "ERROR Cannot open") ||
		strings.Contains(text, "Imported") || strings.Contains(text, "dnsmasq line") {
		t.Errorf("level=warning should keep only warnings and errors, got:\n%s", text)
	}
	text = callTool(t, ftlLogHandler, c, map[string]any{"level": "error"})
	if strings.Contains(text, "Rate-limiting") || !strings.Contains(text, "Cannot open") {
		t.Errorf("level=error should keep only errors, got:\n%s", text)
	}
}

func TestLogs_LinesOutOfRangeIsRefused(t *testing.T) {
	rec := piholeHandler(map[string]any{"/logs/dnsmasq": map[string]any{"log": []any{}, "nextID": 0}})
	c := newTestClient(t, rec)
	callToolExpectError(t, dnsLogHandler, c, map[string]any{"lines": 513.0})
	rec.AssertNone(t, "GET", "/logs/dnsmasq")
}
