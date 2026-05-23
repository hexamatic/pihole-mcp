// Package config loads Pi-hole MCP server configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRequestTimeout = 30 * time.Second
	defaultRateLimit      = 120
)

// defaultAllowedOrigins is the loopback-only allowlist. Matches the
// DNS-rebinding protection prescribed by the MCP 2025-11-25 spec.
var defaultAllowedOrigins = []string{"localhost", "127.0.0.1", "[::1]"}

// Config holds the Pi-hole MCP server configuration.
type Config struct {
	// URL is the base URL of the Pi-hole instance (e.g. "http://192.168.1.2").
	URL string

	// Password is the admin password or application password for the Pi-hole API.
	Password string

	// RequestTimeout is the HTTP request timeout for Pi-hole API calls.
	RequestTimeout time.Duration

	// RateLimit is the per-session request limit per minute for the HTTP/SSE
	// transports. 0 disables rate limiting (not recommended on a LAN).
	RateLimit int

	// AllowedOrigins is the Origin/Host allowlist for the HTTP/SSE transports.
	// Defaults to loopback. The special value "*" disables enforcement.
	AllowedOrigins []string
}

// Load reads configuration from environment variables and validates it.
func Load() (*Config, error) {
	cfg := &Config{
		URL:            os.Getenv("PIHOLE_URL"),
		Password:       os.Getenv("PIHOLE_PASSWORD"),
		RequestTimeout: defaultRequestTimeout,
		RateLimit:      defaultRateLimit,
		AllowedOrigins: append([]string(nil), defaultAllowedOrigins...),
	}

	if cfg.URL == "" {
		return nil, fmt.Errorf("PIHOLE_URL environment variable is required")
	}

	if _, err := url.Parse(cfg.URL); err != nil {
		return nil, fmt.Errorf("PIHOLE_URL is not a valid URL: %w", err)
	}

	if _, ok := os.LookupEnv("PIHOLE_PASSWORD"); !ok {
		return nil, fmt.Errorf("PIHOLE_PASSWORD environment variable is required (set to empty string for no-password Pi-hole instances)")
	}

	if v := os.Getenv("PIHOLE_REQUEST_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("PIHOLE_REQUEST_TIMEOUT is not a valid duration: %w", err)
		}
		cfg.RequestTimeout = d
	}

	if v := os.Getenv("PIHOLE_RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("PIHOLE_RATE_LIMIT is not a valid integer: %w", err)
		}
		if n < 0 {
			return nil, fmt.Errorf("PIHOLE_RATE_LIMIT must be >= 0 (got %d)", n)
		}
		cfg.RateLimit = n
	}

	if v, ok := os.LookupEnv("PIHOLE_ALLOWED_ORIGINS"); ok {
		cfg.AllowedOrigins = parseOrigins(v)
		if len(cfg.AllowedOrigins) == 0 {
			return nil, fmt.Errorf("PIHOLE_ALLOWED_ORIGINS must contain at least one entry (or '*' to disable enforcement)")
		}
	}

	return cfg, nil
}

func parseOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
