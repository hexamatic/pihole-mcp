package pihole

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ftlUptimeServer answers /api/info/ftl with whatever uptime (in ms) the
// supplied function returns for the nth probe, or a 503 when it returns a
// negative number, standing in for FTL while it is down.
func ftlUptimeServer(t *testing.T, uptime func(n int) float64) (*Client, *atomic.Int32) {
	t.Helper()
	var probes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			writeJSON(w, authResponse{Session: sessionInfo{Valid: true, SID: "s"}})
			return
		}
		if r.URL.Path != "/api/info/ftl" {
			http.NotFound(w, r)
			return
		}
		up := uptime(int(probes.Add(1)))
		if up < 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"ftl": map[string]any{"uptime": up}})
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "test", WithRetry(0, time.Second), WithRestartWait(2*time.Second)), &probes
}

func TestAwaitRestart_WaitsThroughTheShutdownForANewProcess(t *testing.T) {
	// The old process answers first, then nothing answers, then the new one
	// reports a short uptime: the sequence a real restart produces.
	c, probes := ftlUptimeServer(t, func(n int) float64 {
		switch {
		case n <= 2:
			return 3_600_000 // the process that took the write, still up
		case n <= 4:
			return -1 // down
		default:
			return 50 // started 50 ms ago
		}
	})

	if err := c.AwaitRestart(context.Background(), time.Now()); err != nil {
		t.Fatalf("AwaitRestart: %v", err)
	}
	if n := probes.Load(); n < 5 {
		t.Errorf("returned after %d probes, before the new process answered", n)
	}
}

func TestAwaitRestart_GivesUpWhenFTLNeverRestarts(t *testing.T) {
	c, _ := ftlUptimeServer(t, func(int) float64 { return 3_600_000 })
	c.restartWait = 300 * time.Millisecond

	err := c.AwaitRestart(context.Background(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "had not finished restarting") {
		t.Fatalf("err = %v, want a timeout that says the restart had not finished", err)
	}
}

func TestRestartsFTL_ReadsFTLsOwnFlag(t *testing.T) {
	_, c := newAuthedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("detailed") != "true" {
			t.Errorf("flag lookup did not ask for detailed output: %s", r.URL)
		}
		leaf := func(restarts bool) map[string]any {
			return map[string]any{"value": []any{}, "flags": map[string]any{"restart_dnsmasq": restarts}}
		}
		switch r.URL.Path {
		case "/api/config/dns/cnameRecords":
			writeJSON(w, map[string]any{"config": map[string]any{"dns": map[string]any{"cnameRecords": leaf(true)}}})
		case "/api/config/dns/hosts":
			writeJSON(w, map[string]any{"config": map[string]any{"dns": map[string]any{"hosts": leaf(false)}}})
		default:
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, errorResponse{Error: errorDetail{Key: "not_found", Message: "Not found"}})
		}
	})

	for element, want := range map[string]bool{"dns.cnameRecords": true, "dns.hosts": false} {
		got, err := c.RestartsFTL(context.Background(), element)
		if err != nil || got != want {
			t.Errorf("RestartsFTL(%s) = %v, %v; want %v", element, got, err, want)
		}
	}
	if _, err := c.RestartsFTL(context.Background(), "dns.nonsense"); err == nil {
		t.Error("an unknown element should be an error, not a silent false")
	}
}

func TestDo_AnUnansweredWriteIsUncertain(t *testing.T) {
	// The server takes the request and drops the connection without a reply,
	// as FTL does when it exits mid-request during a restart.
	_, c := newAuthedServer(t, func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})

	err := c.Post(context.Background(), "/domains/deny/exact", map[string]any{"domain": "a.example"}, nil)
	var uncertain *UncertainError
	if !errors.As(err, &uncertain) {
		t.Fatalf("a POST that lost its reply returned %T %v, want *UncertainError", err, err)
	}

	err = c.Get(context.Background(), "/domains", nil)
	if errors.As(err, &uncertain) {
		t.Errorf("a GET that lost its reply changes nothing and should not be uncertain: %v", err)
	}
}

func TestDo_ARefusedConnectionIsNotUncertain(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens here now

	c := New(url, "test", WithRetry(0, time.Second))
	err := c.Post(context.Background(), "/domains/deny/exact", map[string]any{"domain": "a.example"}, nil)
	var uncertain *UncertainError
	if err == nil || errors.As(err, &uncertain) {
		t.Errorf("a refused connection sent nothing, so it is not uncertain: %T %v", err, err)
	}
}
