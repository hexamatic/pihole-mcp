package main

import (
	"bytes"
	"context"
	"errors"
	"log"
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

func TestIsLoopbackAddress(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"localhost:8080", true},
		{"127.0.0.1:8080", true},
		{"127.0.0.53:8080", true},
		{"[::1]:8080", true},
		{"::1", true},
		{"0.0.0.0:8080", false},
		{"[::]:8080", false},
		{":8080", false},
		{"192.168.1.10:8080", false},
		{"pihole-mcp.invalid:8080", false},
	}
	for _, tt := range tests {
		if got := isLoopbackAddress(tt.address); got != tt.want {
			t.Errorf("isLoopbackAddress(%q) = %v, want %v", tt.address, got, tt.want)
		}
	}
}

// TestWarnIfUnauthenticated pins the one combination that must be loud: a bind
// reachable beyond this machine with no token. Everything else stays quiet, and
// nothing here refuses to start.
func TestWarnIfUnauthenticated(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		authEnabled bool
		wantWarning bool
	}{
		{"non-loopback with no token", "0.0.0.0:8080", false, true},
		{"non-loopback with a token", "0.0.0.0:8080", true, false},
		{"loopback with no token", "localhost:8080", false, false},
		{"loopback with a token", "127.0.0.1:8080", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })

			warnIfUnauthenticated(tt.address, tt.authEnabled)

			logged := buf.String()
			if tt.wantWarning {
				if !strings.Contains(logged, "ERROR") {
					t.Fatalf("want an error-level warning, got %q", logged)
				}
				if !strings.Contains(logged, "PIHOLE_HTTP_AUTH_TOKEN") {
					t.Errorf("the warning must name the variable that fixes it, got %q", logged)
				}
			} else if logged != "" {
				t.Fatalf("want no warning, got %q", logged)
			}
		})
	}
}

func TestAuthState(t *testing.T) {
	if got := authState(true); got != "bearer token" {
		t.Errorf("authState(true) = %q, want %q", got, "bearer token")
	}
	if got := authState(false); got != "none" {
		t.Errorf("authState(false) = %q, want %q", got, "none")
	}
}

func TestIsLoopbackAddress_ResolvedNames(t *testing.T) {
	tests := []struct {
		name    string
		resolve []net.IPAddr
		err     error
		want    bool
	}{
		{"every address is loopback", []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("::1")}}, nil, true},
		{"one address is not", []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("192.168.1.10")}}, nil, false},
		{"resolves to nothing", nil, nil, false},
		{"does not resolve", nil, errors.New("no such host"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := lookupIP
			lookupIP = func(context.Context, string) ([]net.IPAddr, error) { return tt.resolve, tt.err }
			t.Cleanup(func() { lookupIP = original })

			if got := isLoopbackAddress("pihole-mcp.lan:8080"); got != tt.want {
				t.Errorf("isLoopbackAddress = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestServeHTTPInstallsTheMiddlewareChain drives a real listener to assert that
// the server actually wraps the MCP handler in the middleware it is configured
// with, in the right order.
//
// Every middleware here has thorough unit tests, and all of them would keep
// passing if a line were dropped from the Chain in serveHTTPContext. Nothing
// else covers the wiring, which is the part that decides whether any of it runs.
func TestServeHTTPInstallsTheMiddlewareChain(t *testing.T) {
	const token = "placeholder-token-not-real"

	var reached atomic.Int64
	mcpHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	address := freeAddress(t)
	done := make(chan error, 1)
	cfg := &config.Config{
		RateLimit:      120,
		AllowedOrigins: []string{"localhost", "127.0.0.1"},
		HTTPAuthToken:  token,
	}

	ctx, cancel := context.WithCancel(t.Context())
	// Registered first, so it runs last: cancel below stops the server, then
	// this waits for it to finish shutting down.
	defer func() {
		if err := <-done; err != nil {
			t.Errorf("serveHTTPContext returned %v", err)
		}
	}()
	defer cancel()
	go func() { done <- serveHTTPContext(ctx, cfg, address, mcpHandler, func(context.Context) {}) }()
	waitForListener(t, address)

	url := "http://" + address + "/mcp"
	tests := []struct {
		name       string
		host       string
		authHeader string
		want       int
	}{
		{"no credentials", "", "", http.StatusUnauthorized},
		{"wrong credentials", "", "Bearer not-the-configured-token", http.StatusUnauthorized},
		{"host off the allowlist is refused before authentication", "evil.example", "Bearer " + token, http.StatusForbidden},
		{"correct credentials", "", "Bearer " + token, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.host != "" {
				req.Host = tt.host
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}

	if got := reached.Load(); got != 1 {
		t.Fatalf("the MCP handler was reached %d times, want exactly the one authenticated request", got)
	}
}

// waitForListener blocks until address accepts a connection.
func waitForListener(t *testing.T, address string) {
	t.Helper()
	dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
	for i := 0; i < 100; i++ {
		conn, err := dialer.DialContext(t.Context(), "tcp", address)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("nothing listening on %s", address)
}

// TestRunCheckReportsScope covers the scoping half of -check. A user whose
// PIHOLE_TOOLSETS hides the tool they were reaching for sees a server that
// started cleanly and simply lacks it, so the self-test has to say what the
// scoping resolved to and how many tools survived it.
func TestRunCheckReportsScope(t *testing.T) {
	address := freeAddress(t)
	t.Setenv("PIHOLE_URL", "http://"+address)
	t.Setenv("PIHOLE_PASSWORD", "test")
	t.Setenv("PIHOLE_MAX_RETRIES", "0")
	t.Setenv("PIHOLE_READ_ONLY", "true")
	t.Setenv("PIHOLE_TOOLSETS", "domains,stats")

	var out strings.Builder
	runCheck(&out)

	got := out.String()
	// 2 read-only domains tools plus 11 read-only stats tools.
	for _, want := range []string{"scope:", "read-only=true", "domains,stats", "13 of 82 tools exposed"} {
		if !strings.Contains(got, want) {
			t.Errorf("scope line is missing %q:\n%s", want, got)
		}
	}
}

// TestRunCheckReportsUnknownToolset pins the failure path a typo takes. A stdio
// server that exits at startup often shows the user nothing at all, so -check is
// where the typo and the way out of it have to appear.
func TestRunCheckReportsUnknownToolset(t *testing.T) {
	t.Setenv("PIHOLE_URL", "http://127.0.0.1:1")
	t.Setenv("PIHOLE_PASSWORD", "test")
	t.Setenv("PIHOLE_TOOLSETS", "doamins")

	var out strings.Builder
	if runCheck(&out) {
		t.Error("runCheck reported success with an unknown toolset name")
	}
	got := out.String()
	for _, want := range []string{"FAIL", "PIHOLE_TOOLSETS", "doamins", "domains"} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}
}
