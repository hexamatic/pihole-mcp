// Pi-hole MCP Server — MCP (Model Context Protocol) server for Pi-hole v6.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/config"
	"github.com/hexamatic/pihole-mcp/internal/middleware"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	piholeserver "github.com/hexamatic/pihole-mcp/internal/server"
	"github.com/hexamatic/pihole-mcp/internal/telemetry"
	"github.com/mark3labs/mcp-go/server"
)

const (
	httpReadHeaderTimeout = 10 * time.Second
	shutdownGracePeriod   = 5 * time.Second
)

func main() {
	version := flag.Bool("version", false, "Print version and exit")
	transport := flag.String("transport", "stdio", "Transport type: stdio, http, or sse")
	address := flag.String("address", "localhost:8080", "Listen address for http/sse transports")
	flag.Parse()

	if *version {
		fmt.Println("pihole-mcp " + piholeserver.Version)
		return
	}

	log.SetOutput(os.Stderr)

	if err := run(*transport, *address); err != nil {
		log.Fatal(err)
	}
}

func run(transport, address string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	instances := make([]pihole.InstanceConfig, len(cfg.Instances))
	for i, ic := range cfg.Instances {
		instances[i] = pihole.InstanceConfig{Name: ic.Name, URL: ic.URL, Password: ic.Password}
	}
	registry := pihole.NewRegistry(instances, pihole.WithTimeout(cfg.RequestTimeout))
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
		return serveHTTP(cfg, address, server.NewStreamableHTTPServer(srv))

	case "sse":
		return serveHTTP(cfg, address, server.NewSSEServer(srv))

	default:
		return fmt.Errorf("unknown transport: %s (expected stdio, http, or sse)", transport)
	}
}

// serveHTTP wraps an MCP HTTP/SSE handler with the configured middleware
// chain and runs it with graceful shutdown on SIGINT/SIGTERM.
func serveHTTP(cfg *config.Config, address string, mcpHandler http.Handler) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
		defer shutdownCancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
