// Pi-hole MCP Server — MCP (Model Context Protocol) server for Pi-hole v6.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	rl := middleware.NewRateLimiter(cfg.RateLimit, middleware.ComputeBurst(cfg.RateLimit))
	rl.BindShutdown(ctx)
	ov := middleware.NewOriginValidator(cfg.AllowedOrigins)

	handler := middleware.Chain(
		ov.Middleware,
		rl.Middleware,
	)(mcpHandler)

	s := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: httpReadHeaderTimeout,
	}

	log.Printf("starting %T on %s (rate-limit=%d/min, allowed-origins=%v)",
		mcpHandler, address, cfg.RateLimit, cfg.AllowedOrigins)

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
