// Pi-hole MCP Server — MCP (Model Context Protocol) server for Pi-hole v6.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// Embed the IANA timezone database so TZ resolves even where the OS
	// provides no zoneinfo (distroless/scratch containers, Windows).
	_ "time/tzdata"

	"github.com/hexamatic/pihole-mcp/internal/config"
	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/middleware"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	piholeserver "github.com/hexamatic/pihole-mcp/internal/server"
	"github.com/hexamatic/pihole-mcp/internal/telemetry"
	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/server"
)

const (
	httpReadHeaderTimeout = 10 * time.Second
	shutdownGracePeriod   = 5 * time.Second

	// drainInterval is how often shutdown re-closes MCP sessions. Sessions
	// registered after the first sweep would otherwise hold a long-lived GET
	// open until the grace period expired.
	drainInterval = 5 * time.Millisecond

	// hostLookupTimeout bounds the name resolution behind the startup
	// authentication warning, so a slow or broken resolver delays the log line
	// rather than the server.
	hostLookupTimeout = 2 * time.Second
)

func main() {
	version := flag.Bool("version", false, "Print version and exit")
	check := flag.Bool("check", false, "Check the configuration and connectivity to every Pi-hole, then exit")
	transport := flag.String("transport", "stdio", "Transport type: stdio, http, or sse")
	address := flag.String("address", "localhost:8080", "Listen address for http/sse transports")
	flag.Parse()

	if *version {
		fmt.Println("pihole-mcp " + piholeserver.Version)
		return
	}

	log.SetOutput(os.Stderr)

	if *check {
		if !runCheck(os.Stdout) {
			os.Exit(1)
		}
		return
	}

	if err := run(*transport, *address); err != nil {
		log.Fatal(err)
	}
}

// newRegistry builds the Pi-hole client registry described by cfg.
func newRegistry(cfg *config.Config) *pihole.Registry {
	instances := make([]pihole.InstanceConfig, len(cfg.Instances))
	for i, ic := range cfg.Instances {
		instances[i] = pihole.InstanceConfig{Name: ic.Name, URL: ic.URL, Password: ic.Password}
	}
	return pihole.NewRegistry(instances,
		pihole.WithTimeout(cfg.RequestTimeout),
		pihole.WithRetry(cfg.MaxRetries, cfg.RetryMaxDelay),
		pihole.WithTLSSkipVerify(cfg.TLSSkipVerify),
	)
}

// runCheck implements -check: load the configuration, then make one
// authenticated request against every configured Pi-hole and report the result.
// It reports whether every instance answered.
//
// Almost every "it does not work" report against an MCP server is a
// configuration or reachability problem, and the client that launched the
// server usually swallows its stderr, so the user sees a tool that is simply
// absent. Running the binary directly with this flag turns that into a sentence.
func runCheck(out io.Writer) bool {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	say("pihole-mcp %s\n\n", piholeserver.Version)

	cfg, err := config.Load()
	if err != nil {
		say("FAIL  configuration: %v\n", err)
		return false
	}

	registry := newRegistry(cfg)
	defer registry.Close()

	ok := true
	for _, ic := range cfg.Instances {
		client, err := registry.Get(ic.Name)
		if err != nil {
			say("FAIL  %s (%s)\n      %v\n", ic.Name, ic.URL, err)
			ok = false
			continue
		}

		var ver pihole.VersionInfo
		if err := client.Get(context.Background(), "/info/version", &ver); err != nil {
			say("FAIL  %s (%s)\n      %v\n", ic.Name, ic.URL, err)
			if hint := tools.TransportHint(err); hint != "" {
				say("      %s\n", hint)
			}
			ok = false
			continue
		}

		core := ver.Version.Core.Local.Version
		if core == "" {
			core = "unknown"
		}
		ftl := ver.Version.FTL.Local.Version
		if ftl == "" {
			ftl = "unknown"
		}
		say("PASS  %s (%s)  core %s, FTL %s\n", ic.Name, ic.URL, core, ftl)
	}

	say("\n")
	if !ok {
		say("One or more instances failed. Fix the reported errors, then run this again.\n")
		return false
	}
	say("All %d instance(s) reachable and authenticated.\n", len(cfg.Instances))
	return true
}

func run(transport, address string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	loc, tzErr := config.TimeLocation()
	if tzErr != nil {
		log.Printf("warning: %v — timestamps will use %s", tzErr, loc)
	}
	format.SetLocation(loc)

	registry := newRegistry(cfg)
	defer registry.Close()

	srv := piholeserver.New(registry)

	tp, err := telemetry.Init("pihole-mcp", piholeserver.Version)
	if err != nil {
		return fmt.Errorf("telemetry init error: %w", err)
	}
	if tp != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
			defer cancel()
			_ = tp.Shutdown(ctx)
		}()
	}

	switch transport {
	case "stdio":
		return server.ServeStdio(srv)

	case "http":
		h := server.NewStreamableHTTPServer(srv)
		return serveHTTP(cfg, address, h, h.CloseSessions)

	case "sse":
		h := server.NewSSEServer(srv)
		return serveHTTP(cfg, address, h, func(context.Context) { h.CloseSessions() })

	default:
		return fmt.Errorf("unknown transport: %s (expected stdio, http, or sse)", transport)
	}
}

// serveHTTP wraps an MCP HTTP/SSE handler with the configured middleware
// chain and runs it with graceful shutdown on SIGINT/SIGTERM.
//
// closeSessions terminates the transport's MCP sessions. Both transports have
// the method, with different signatures, and neither transport's own Shutdown
// can be used here because the listener belongs to the http.Server this
// function builds to carry the origin and rate-limit middleware.
func serveHTTP(cfg *config.Config, address string, mcpHandler http.Handler, closeSessions func(context.Context)) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return serveHTTPContext(ctx, cfg, address, mcpHandler, closeSessions)
}

// serveHTTPContext is serveHTTP with the shutdown trigger supplied by the
// caller. Splitting it out keeps the signal handling out of the tests, which
// would otherwise have to raise a real SIGTERM and could not run on Windows.
func serveHTTPContext(ctx context.Context, cfg *config.Config, address string, mcpHandler http.Handler, closeSessions func(context.Context)) error {
	rl := middleware.NewRateLimiter(cfg.RateLimit, middleware.ComputeBurst(cfg.RateLimit),
		middleware.WithTrustedProxies(cfg.TrustedProxies))
	rl.BindShutdown(ctx)
	ov := middleware.NewOriginValidator(cfg.AllowedOrigins)
	// Failed authentication is charged to its own budget rather than the one
	// above, so a flood of wrong tokens throttles the sender without starving a
	// client at the same address that does hold the token.
	authFailures := middleware.NewFailureLimiter(middleware.WithTrustedProxies(cfg.TrustedProxies))
	authFailures.BindShutdown(ctx)
	auth := middleware.NewBearerAuth(cfg.HTTPAuthToken, middleware.WithFailurePenalty(authFailures.Penalise))

	warnIfUnauthenticated(address, auth.Enabled())

	// Authentication sits ahead of the rate limiter deliberately: a caller with
	// no token must not be able to spend the budget of one that has it.
	handler := middleware.Chain(
		ov.Middleware,
		auth.Middleware,
		rl.Middleware,
	)(mcpHandler)

	s := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: httpReadHeaderTimeout,
	}

	log.Printf("starting %T on %s (auth=%s, rate-limit=%d/min, allowed-origins=%v, trusted-proxies=%v)",
		mcpHandler, address, authState(auth.Enabled()), cfg.RateLimit, cfg.AllowedOrigins, cfg.TrustedProxies)

	// ListenAndServe returns as soon as Shutdown closes the listener, so
	// without this channel the process exited while the drain was still running
	// and the advertised grace period never actually applied to anything.
	done := make(chan struct{})
	// The grace period has to outlive ctx: ctx is the signal context, and it is
	// already cancelled by the time this goroutine wakes up.
	go func() { //nolint:gosec // G118: a fresh deadline is the point, see above
		defer close(done)
		<-ctx.Done()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
		defer shutdownCancel()

		log.Printf("shutting down, draining connections (grace period %s)", shutdownGracePeriod)
		start := time.Now()
		if err := drain(shutdownCtx, s, closeSessions); err != nil {
			log.Printf("shutdown incomplete after %s: %v", time.Since(start).Round(time.Millisecond), err)
			return
		}
		log.Printf("shutdown complete in %s", time.Since(start).Round(time.Millisecond))
	}()

	if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		// The listener never came up (address in use, permission denied). The
		// goroutine above is still parked on a signal that is not coming, so
		// waiting on done here would hang instead of reporting the error.
		return err
	}
	<-done
	return nil
}

// drain closes the transport's MCP sessions and shuts the HTTP server down,
// re-closing sessions on a ticker until the shutdown completes.
//
// A long-lived GET is how both MCP HTTP transports deliver server-to-client
// messages, and it does not end on its own: http.Server.Shutdown waits for it,
// so without closing the sessions first every shutdown burned the whole grace
// period and then killed the stream mid-flight. The repeat is not belt and
// braces. A session that registers between the first sweep and the listener
// closing would otherwise hold the shutdown open on its own, which is why
// mcp-go's own StreamableHTTPServer.Shutdown drains on the same ticker.
func drain(ctx context.Context, s *http.Server, closeSessions func(context.Context)) error {
	closeSessions(ctx)

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- s.Shutdown(ctx) }()

	ticker := time.NewTicker(drainInterval)
	defer ticker.Stop()

	for {
		select {
		case err := <-shutdownDone:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			closeSessions(ctx)
		}
	}
}

func authState(enabled bool) string {
	if enabled {
		return "bearer token"
	}
	return "none"
}

// warnIfUnauthenticated reports the one combination that hands an unauthenticated
// MCP server to a whole network: a listen address that is not loopback with no
// bearer token configured.
//
// It warns rather than refusing to start. Refusing would break the reverse-proxy
// deployment the README documents, where the proxy in front holds the
// authentication and pihole-mcp binds 0.0.0.0 inside a container on purpose. The
// operator who meant it loses nothing; the operator who did not gets told, in the
// place they are already looking when a new server does not behave.
func warnIfUnauthenticated(address string, authEnabled bool) {
	if authEnabled || isLoopbackAddress(address) {
		return
	}
	log.Printf("ERROR: listening on %s, which is reachable beyond this machine, with no authentication. "+
		"Anyone who can reach this port can control your Pi-hole. Set PIHOLE_HTTP_AUTH_TOKEN "+
		"(or PIHOLE_HTTP_AUTH_TOKEN_FILE), or bind localhost, or put a reverse proxy that authenticates in front. "+
		"Origin and Host validation does not authenticate anything: both headers are set by the caller.", address)
}

// lookupIP resolves a hostname. A variable so tests can exercise the resolved
// branches of isLoopbackAddress without depending on the machine's DNS.
var lookupIP = net.DefaultResolver.LookupIPAddr

// isLoopbackAddress reports whether a listen address reaches only this machine.
//
// A bare port or a wildcard host ("", "0.0.0.0", "::") listens on every
// interface. Anything that will not resolve to loopback addresses is treated as
// not loopback, so an address this cannot classify produces a warning rather
// than silence.
func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = strings.Trim(address, "[]")
	}
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	// Resolved without asking anyone: a host with no "localhost" entry, or a
	// resolver that is slow or broken, must not turn the default bind address
	// into a spurious "no authentication" warning.
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostLookupTimeout)
	defer cancel()
	ips, err := lookupIP(ctx, host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() {
			return false
		}
	}
	return true
}
