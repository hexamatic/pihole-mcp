package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/config"
)

func TestRun_MissingConfig(t *testing.T) {
	t.Setenv("PIHOLE_URL", "")
	t.Setenv("PIHOLE_PASSWORD", "")

	err := run("stdio", "localhost:0")
	if err == nil {
		t.Fatal("expected configuration error with empty PIHOLE_URL")
	}
	if !strings.Contains(err.Error(), "configuration error") {
		t.Errorf("err = %v, want configuration error", err)
	}
}

func TestRun_UnknownTransport(t *testing.T) {
	t.Setenv("PIHOLE_URL", "http://pihole.invalid")
	t.Setenv("PIHOLE_PASSWORD", "x")

	err := run("carrier-pigeon", "localhost:0")
	if err == nil {
		t.Fatal("expected error for unknown transport")
	}
	if !strings.Contains(err.Error(), "unknown transport") ||
		!strings.Contains(err.Error(), "carrier-pigeon") {
		t.Errorf("err = %v, want unknown-transport error naming the input", err)
	}
}

func TestRun_InvalidTimezoneStillStarts(t *testing.T) {
	// An unloadable TZ must not prevent startup (it warns and falls back);
	// the unknown transport proves run() got past config and TZ resolution.
	t.Setenv("PIHOLE_URL", "http://pihole.invalid")
	t.Setenv("PIHOLE_PASSWORD", "x")
	t.Setenv("TZ", "Not/AZone")

	err := run("bogus", "localhost:0")
	if err == nil || !strings.Contains(err.Error(), "unknown transport") {
		t.Errorf("err = %v, want unknown-transport (i.e. TZ handled non-fatally)", err)
	}
}

// freeAddress returns a loopback address nothing is listening on.
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

// TestServeHTTPDrainsBeforeReturning is the regression test for a shutdown that
// advertised a five second grace period and gave connections none of it.
//
// ListenAndServe returns the moment Shutdown closes the listener, so the old
// code returned from serveHTTP, and the process exited, while the drain was
// still in flight in a goroutine nobody waited on. A client holding the
// long-lived GET that both MCP HTTP transports use for server-to-client
// messages had its connection cut mid-message. This asserts the two halves of
// the fix: sessions are closed so the drain can finish at all, and serveHTTP
// does not return until it has.
func TestServeHTTPDrainsBeforeReturning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var handlerReturned atomic.Bool
	var closes atomic.Int64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release // stands in for a long-lived MCP GET, which never ends on its own
		handlerReturned.Store(true)
	})

	// Closing the session is what lets the handler return, exactly as
	// CloseSessions unblocks a real transport's streaming handler.
	closeSessions := func(context.Context) {
		if closes.Add(1) == 1 {
			close(release)
		}
	}

	cfg := &config.Config{RateLimit: 0, AllowedOrigins: []string{"*"}}
	address := freeAddress(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	served := make(chan error, 1)
	go func() { served <- serveHTTPContext(ctx, cfg, address, handler, closeSessions) }()

	// Retry the connect: ListenAndServe binds asynchronously, so the first
	// dial can beat the listener.
	go func() {
		for i := 0; i < 100; i++ {
			resp, err := http.Get("http://" + address + "/") //nolint:noctx // the request is meant to hang
			if err == nil {
				_ = resp.Body.Close()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	select {
	case <-started:
	case <-time.After(20 * time.Second):
		t.Fatal("handler never ran — the server did not come up")
	}

	cancel()

	select {
	case err := <-served:
		if err != nil {
			t.Errorf("serveHTTPContext returned %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("serveHTTPContext never returned")
	}

	if closes.Load() == 0 {
		t.Error("shutdown never closed the transport's sessions, so a streaming client is cut off rather than drained")
	}
	if !handlerReturned.Load() {
		t.Error("serveHTTPContext returned while a request was still in flight — the grace period is not being applied")
	}
}

// TestServeHTTPReportsListenFailure guards the other side of the done channel:
// when the listener never comes up there is no shutdown to wait for, and
// blocking on it would hang instead of reporting the error.
func TestServeHTTPReportsListenFailure(t *testing.T) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	cfg := &config.Config{RateLimit: 0, AllowedOrigins: []string{"*"}}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	served := make(chan error, 1)
	go func() {
		served <- serveHTTPContext(context.Background(), cfg, l.Addr().String(), handler, func(context.Context) {})
	}()

	select {
	case err := <-served:
		if err == nil {
			t.Error("binding an address already in use returned no error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveHTTPContext hung instead of reporting the bind failure")
	}
}

// TestRunCheckReportsUnreachableInstance covers the -check failure path: a
// configured but unreachable Pi-hole must be reported by name and URL and must
// not exit zero, because a self-test that passes regardless is worse than none.
func TestRunCheckReportsUnreachableInstance(t *testing.T) {
	address := freeAddress(t)
	t.Setenv("PIHOLE_URL", "http://"+address)
	t.Setenv("PIHOLE_PASSWORD", "test")
	t.Setenv("PIHOLE_MAX_RETRIES", "0")

	var out strings.Builder
	if runCheck(&out) {
		t.Error("runCheck reported success against an address with nothing listening")
	}

	got := out.String()
	for _, want := range []string{"FAIL", "primary", address} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not mention %q, so the user cannot tell which instance failed:\n%s", want, got)
		}
	}
}

// TestRunCheckReportsConfigurationError covers the earlier failure: no
// configuration at all must be reported rather than crashing or passing.
func TestRunCheckReportsConfigurationError(t *testing.T) {
	t.Setenv("PIHOLE_URL", "")
	_ = os.Unsetenv("PIHOLE_URL")

	var out strings.Builder
	if runCheck(&out) {
		t.Error("runCheck reported success with no configuration")
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Errorf("configuration failure was not reported as FAIL:\n%s", out.String())
	}
}
