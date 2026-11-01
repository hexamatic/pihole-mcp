package pihole

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	oldStart := time.Now().Add(-time.Hour)
	var newStart time.Time
	c, probes := ftlUptimeServer(t, func(n int) float64 {
		switch {
		case n <= 2:
			return msSince(oldStart) // the process that took the write, still up
		case n <= 4:
			newStart = time.Now()
			return -1 // down
		default:
			return msSince(newStart)
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
	started := time.Now().Add(-time.Hour)
	c, _ := ftlUptimeServer(t, func(int) float64 { return msSince(started) })
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

func TestAwaitRestart_ASlowReplyFromTheOldProcessIsNotARestart(t *testing.T) {
	// FTL last started just before the write, its replies take longer to
	// arrive than that, and one probe fails without FTL having gone anywhere.
	// Measured once the reply was in, its uptime looked shorter than the
	// time since the write; and counting the failed probe as a restart in
	// progress let it pass for the new process.
	sent := time.Now()
	started := sent.Add(-20 * time.Millisecond)
	c, _ := ftlUptimeServerWith(t, func(n int) (float64, time.Duration) {
		if n == 2 {
			return -1, 0 // a transient error
		}
		return msSince(started), 80 * time.Millisecond
	})
	c.restartWait = 900 * time.Millisecond

	if err := c.AwaitRestart(context.Background(), sent); err == nil {
		t.Fatal("an FTL that started before the write was taken for a restart")
	}
}

func TestAwaitRestart_AQuickRestartIsConfirmedOnceADifferentProcessAnswers(t *testing.T) {
	// The new process started 5 ms after the write, sooner than a probe takes
	// to come back, so its uptime is never below the time since the write
	// as measured when the probe was sent. It started after the process that
	// answered the first probe, though, which only a restart explains.
	sent := time.Now()
	started := sent.Add(5 * time.Millisecond)
	c, probes := ftlUptimeServerWith(t, func(n int) (float64, time.Duration) {
		switch n {
		case 1:
			return msSince(sent.Add(-time.Hour)), 0 // the process that took the write
		case 2:
			return -1, 0 // down
		}
		time.Sleep(20 * time.Millisecond) // FTL is slow to answer
		return msSince(started), 0
	})

	if err := c.AwaitRestart(context.Background(), sent); err != nil {
		t.Fatalf("AwaitRestart: %v", err)
	}
	if n := probes.Load(); n != 3 {
		t.Errorf("confirmed after %d probes, want 3", n)
	}
}

// ftlUptimeServerWith is ftlUptimeServer with a delay after the uptime is
// measured and before the reply is sent.
func ftlUptimeServerWith(t *testing.T, probe func(n int) (float64, time.Duration)) (*Client, *atomic.Int32) {
	t.Helper()
	var probes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			writeJSON(w, authResponse{Session: sessionInfo{Valid: true, SID: "s"}})
			return
		}
		up, delay := probe(int(probes.Add(1)))
		if up < 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		time.Sleep(delay)
		writeJSON(w, map[string]any{"ftl": map[string]any{"uptime": up}})
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "test", WithRetry(0, time.Second), WithRestartWait(2*time.Second)), &probes
}

func TestDo_AWriteCancelledInFlightIsUncertain(t *testing.T) {
	// The client cancels the tool call after the write reached Pi-hole. The
	// reply is abandoned, but the change may have been made.
	received, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	_, c := newAuthedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, map[string]any{})
			return
		}
		once.Do(func() { close(received) })
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	defer close(release)
	// Log in first, so the cancelled call below fails on the write itself.
	if err := c.Get(context.Background(), "/info/version", nil); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-received; cancel() }()
	err := c.Post(ctx, "/domains/deny/exact", map[string]any{"domain": "a.example"}, nil)
	var uncertain *UncertainError
	if !errors.As(err, &uncertain) {
		t.Fatalf("a POST cancelled after it was sent returned %T %v, want *UncertainError", err, err)
	}

	// A context that had already ended stopped the request before it went.
	err = c.Post(ctx, "/domains/deny/exact", map[string]any{"domain": "a.example"}, nil)
	if err == nil || errors.As(err, &uncertain) {
		t.Errorf("a POST with an ended context sent nothing, so it is not uncertain: %T %v", err, err)
	}
}

func TestDetailedConfigItem_MatchesKeysAsFTLDoes(t *testing.T) {
	cfg := map[string]any{"dns": map[string]any{"cache": map[string]any{
		"size": map[string]any{"value": 10000.0, "flags": map[string]any{"restart_dnsmasq": true}},
	}}}
	item, ok := DetailedConfigItem(cfg, "DNS.Cache.size")
	if !ok || item.Key != "dns.cache.size" || !item.RestartsFTL {
		t.Errorf("DetailedConfigItem(DNS.Cache.size) = %+v, %v; want dns.cache.size, which FTL matches ignoring case", item, ok)
	}
}

// msSince is an uptime as FTL reports it: milliseconds since start, to the
// microsecond, growing with the clock.
func msSince(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}
