package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/server"
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
		// An uptime of zero is a process that has just started.
		"/info/ftl": map[string]any{"ftl": map[string]any{"uptime": 0}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, actionRestartDNSHandler, c, nil)
	if !strings.Contains(text, "DNS restarted") || !strings.Contains(text, "answering again") {
		t.Errorf("expected the reply to confirm Pi-hole came back, got: %s", text)
	}

	// Restarting DNS interrupts resolution for every client on the network, so
	// this must be exactly one POST to the restart endpoint: a duplicate is a
	// second outage the reply would never mention. The GETs that follow are the
	// wait for FTL to come back.
	rec.Only(t, "POST", "/action/restartdns").AssertNoQueryString(t)
	rec.AssertCount(t, "POST", "", 1)
	if rec.Count("GET", "/info/ftl") == 0 {
		t.Error("the tool reported the restart without checking that FTL came back")
	}
}

func TestActionRestartDNS_NotBackInTimeIsAnError(t *testing.T) {
	// FTL answers before it restarts. An uptime that never resets is a
	// restart that has not happened, or has not finished.
	srv := httptest.NewServer(piholeHandler(map[string]any{
		"/action/restartdns": map[string]any{"success": true},
		"/info/ftl":          map[string]any{"ftl": map[string]any{"uptime": 3_600_000}},
	}))
	t.Cleanup(srv.Close)
	c := pihole.New(srv.URL, "test", pihole.WithRestartWait(300*time.Millisecond))

	text := callToolExpectError(t, actionRestartDNSHandler, c, nil)
	if !strings.Contains(text, "had not finished restarting") {
		t.Errorf("a restart that never completes should say so, got: %s", text)
	}
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

// gravityStream serves /api/action/gravity with the given chunks, flushing
// each and pausing between them, so a test can shape the stream in time.
func gravityStream(chunks []string, pause time.Duration, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			writeTestJSON(w, map[string]any{"session": map[string]any{"valid": true, "sid": "s"}})
			return
		}
		if status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"key":"gravity_running","message":"Gravity is already running"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fl, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(pause)
		}
	})
}

func TestActionGravity_OutlivesTheRequestTimeout(t *testing.T) {
	// The request timeout used to cover the whole rebuild, so every run longer
	// than 30 seconds was reported as failed while Pi-hole finished it anyway.
	srv := httptest.NewServer(gravityStream([]string{"  [i] Pulling\n", "  [i] Building tree\n", "  [✓] Done.\n"}, 150*time.Millisecond, http.StatusOK))
	t.Cleanup(srv.Close)
	c := pihole.New(srv.URL, "test", pihole.WithTimeout(100*time.Millisecond))

	text := callTool(t, actionGravityHandler, c, nil)
	if !strings.Contains(text, "Gravity update complete") || !strings.Contains(text, "Done.") {
		t.Errorf("a rebuild longer than the request timeout should complete, got: %s", text)
	}
}

func TestActionGravity_FailedLineIsAnError(t *testing.T) {
	c := newTestClient(t, gravityStream([]string{
		"  [i] Pulling\n",
		"  [✗] Unable to build gravity tree\n",
		"Gravity failed\n",
	}, 0, http.StatusOK))

	text := callToolExpectError(t, actionGravityHandler, c, nil)
	for _, want := range []string{"Gravity update failed", "Unable to build gravity tree"} {
		if !strings.Contains(text, want) {
			t.Errorf("failed rebuild should mention %q, got: %s", want, text)
		}
	}
}

func TestActionGravity_ListFailuresAreSummarised(t *testing.T) {
	c := newTestClient(t, gravityStream([]string{
		"  [i] Target: https://lists.example/ads.txt\n",
		"  [✗] Status: Not found\n",
		"  [✓] Done.\n",
	}, 0, http.StatusOK))

	text := callTool(t, actionGravityHandler, c, nil)
	if !strings.Contains(text, "Pi-hole reported 1 problem") || !strings.Contains(text, "- [✗] Status: Not found") {
		t.Errorf("a list that failed should be listed ahead of the output, got: %s", text)
	}
}

func TestActionGravity_AlreadyRunning(t *testing.T) {
	c := newTestClient(t, gravityStream(nil, 0, http.StatusConflict))

	text := callToolExpectError(t, actionGravityHandler, c, nil)
	if !strings.Contains(text, "already running") || !strings.Contains(text, "rather than calling this again") {
		t.Errorf("FTL's 409 should explain that a rebuild is in progress, got: %s", text)
	}
}

func TestActionGravity_QuietStreamIsAnUnknownOutcome(t *testing.T) {
	old := gravityIdleTimeout
	gravityIdleTimeout = 100 * time.Millisecond
	t.Cleanup(func() { gravityIdleTimeout = old })

	c := newTestClient(t, gravityStream([]string{"  [i] Pulling\n", "  [i] Parsing\n"}, 400*time.Millisecond, http.StatusOK))

	text := callToolExpectError(t, actionGravityHandler, c, nil)
	for _, want := range []string{"may still be running", "Pulling", "rather than starting another run"} {
		if !strings.Contains(text, want) {
			t.Errorf("a stream that goes quiet should report an unknown outcome mentioning %q, got: %s", want, text)
		}
	}
}

func TestActionGravity_IsNotMarkedIdempotent(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.0")
	RegisterAll(srv, dummyRegistry(1))
	tool := srv.GetTool("pihole_action_gravity_update")
	if tool == nil {
		t.Fatal("gravity tool not registered")
	}
	if h := tool.Tool.Annotations.IdempotentHint; h != nil && *h {
		t.Error("gravity must not be marked idempotent: a replay after an unclear outcome rebuilds again")
	}
}
