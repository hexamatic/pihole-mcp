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
	defaultMaxRetries     = 3
	defaultRetryMaxDelay  = 8 * time.Second
)

// defaultAllowedOrigins is the loopback-only allowlist. Matches the
// DNS-rebinding protection prescribed by the MCP 2025-11-25 spec.
var defaultAllowedOrigins = []string{"localhost", "127.0.0.1", "[::1]"}

// InstanceConfig describes a single Pi-hole instance.
type InstanceConfig struct {
	// Name is the instance identifier used in the "instance" tool argument and
	// in aggregated output. "primary" for single-instance setups.
	Name string

	// URL is the base URL of the Pi-hole instance (e.g. "http://192.168.1.2").
	URL string

	// Password is the admin password or application password for the Pi-hole API.
	Password string
}

// Config holds the Pi-hole MCP server configuration.
type Config struct {
	// Instances is the ordered list of configured Pi-hole instances. There is
	// always at least one; Instances[0] is the default.
	Instances []InstanceConfig

	// RequestTimeout is the HTTP request timeout for Pi-hole API calls.
	RequestTimeout time.Duration

	// RateLimit is the per-session request limit per minute for the HTTP/SSE
	// transports. 0 disables rate limiting (not recommended on a LAN).
	RateLimit int

	// AllowedOrigins is the Origin/Host allowlist for the HTTP/SSE transports.
	// Defaults to loopback. The special value "*" disables enforcement.
	AllowedOrigins []string

	// MaxRetries is how many times a failed Pi-hole API call is re-attempted
	// after the first try. 0 disables retrying.
	MaxRetries int

	// RetryMaxDelay caps how long a single backoff wait may last.
	RetryMaxDelay time.Duration

	// TLSSkipVerify disables TLS certificate verification for Pi-hole API
	// connections. Off by default; intended only for instances serving
	// self-signed certificates.
	TLSSkipVerify bool
}

// Load reads configuration from environment variables and validates it.
//
// Two mutually-exclusive forms are accepted:
//   - Single instance: PIHOLE_URL + PIHOLE_PASSWORD (named "primary").
//   - Multi instance: PIHOLE_1_URL, PIHOLE_1_PASSWORD, optional PIHOLE_1_NAME,
//     then PIHOLE_2_* and so on. The scan stops at the first missing
//     PIHOLE_<n>_URL, so instance numbers must be contiguous from 1.
//
// Setting both forms, or neither, is an error.
func Load() (*Config, error) {
	cfg := &Config{
		RequestTimeout: defaultRequestTimeout,
		RateLimit:      defaultRateLimit,
		AllowedOrigins: append([]string(nil), defaultAllowedOrigins...),
		MaxRetries:     defaultMaxRetries,
		RetryMaxDelay:  defaultRetryMaxDelay,
	}

	instances, err := loadInstances()
	if err != nil {
		return nil, err
	}
	cfg.Instances = instances

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

	if v := os.Getenv("PIHOLE_MAX_RETRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("PIHOLE_MAX_RETRIES is not a valid integer: %w", err)
		}
		if n < 0 {
			return nil, fmt.Errorf("PIHOLE_MAX_RETRIES must be >= 0 (got %d)", n)
		}
		cfg.MaxRetries = n
	}

	if v := os.Getenv("PIHOLE_TLS_SKIP_VERIFY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("PIHOLE_TLS_SKIP_VERIFY is not a valid boolean (use true or false): %w", err)
		}
		cfg.TLSSkipVerify = b
	}

	if v := os.Getenv("PIHOLE_RETRY_MAX_DELAY"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("PIHOLE_RETRY_MAX_DELAY is not a valid duration: %w", err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("PIHOLE_RETRY_MAX_DELAY must be positive (got %s)", d)
		}
		cfg.RetryMaxDelay = d
	}

	return cfg, nil
}

// TimeLocation resolves the display timezone from the TZ environment
// variable (IANA name, e.g. "Australia/Adelaide"). A leading ":" is
// trimmed per POSIX convention. Unset or empty TZ yields time.Local.
//
// Unlike other configuration errors, an unloadable TZ is returned as a
// warning alongside a usable fallback (time.Local) rather than failing
// startup: TZ is an ambient variable often inherited from the host rather
// than set for this server, and Go cannot parse POSIX rule strings (e.g.
// "AEST-10AEDT,M10.1.0,M4.1.0") that are legitimate for other software.
func TimeLocation() (*time.Location, error) {
	tz, ok := os.LookupEnv("TZ")
	if !ok || tz == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(strings.TrimPrefix(tz, ":"))
	if err != nil {
		return time.Local, fmt.Errorf("TZ %q is not a recognised IANA timezone (e.g. \"Australia/Adelaide\"): %w", tz, err)
	}
	return loc, nil
}

// loadInstances resolves the single- or multi-instance environment form.
func loadInstances() ([]InstanceConfig, error) {
	_, singleSet := os.LookupEnv("PIHOLE_URL")
	_, multiSet := os.LookupEnv("PIHOLE_1_URL")

	switch {
	case singleSet && multiSet:
		return nil, fmt.Errorf("set either PIHOLE_URL (single instance) or PIHOLE_1_URL... (multiple instances), not both")
	case singleSet:
		ic, err := loadInstance("PIHOLE", "primary")
		if err != nil {
			return nil, err
		}
		return []InstanceConfig{ic}, nil
	case multiSet:
		return loadNumberedInstances()
	default:
		return nil, fmt.Errorf("PIHOLE_URL (single instance) or PIHOLE_1_URL... (multiple instances) is required")
	}
}

// loadNumberedInstances scans PIHOLE_1_*, PIHOLE_2_*, ... until the first
// missing PIHOLE_<n>_URL.
func loadNumberedInstances() ([]InstanceConfig, error) {
	var instances []InstanceConfig
	seen := make(map[string]bool)
	for i := 1; ; i++ {
		prefix := fmt.Sprintf("PIHOLE_%d", i)
		if _, ok := os.LookupEnv(prefix + "_URL"); !ok {
			break
		}
		defaultName := fmt.Sprintf("instance-%d", i)
		if name := os.Getenv(prefix + "_NAME"); name != "" {
			defaultName = name
		}
		ic, err := loadInstance(prefix, defaultName)
		if err != nil {
			return nil, err
		}
		if seen[ic.Name] {
			return nil, fmt.Errorf("duplicate instance name %q; set a unique %s_NAME", ic.Name, prefix)
		}
		seen[ic.Name] = true
		instances = append(instances, ic)
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no Pi-hole instances configured")
	}
	return instances, nil
}

// loadInstance reads <prefix>_URL and <prefix>_PASSWORD into an InstanceConfig.
func loadInstance(prefix, name string) (InstanceConfig, error) {
	rawURL := os.Getenv(prefix + "_URL")
	if rawURL == "" {
		return InstanceConfig{}, fmt.Errorf("%s_URL is required and must not be empty", prefix)
	}
	if _, err := url.Parse(rawURL); err != nil {
		return InstanceConfig{}, fmt.Errorf("%s_URL is not a valid URL: %w", prefix, err)
	}
	pw, ok := os.LookupEnv(prefix + "_PASSWORD")
	if !ok {
		return InstanceConfig{}, fmt.Errorf("%s_PASSWORD is required (set to empty string for no-password Pi-hole instances)", prefix)
	}
	return InstanceConfig{Name: name, URL: rawURL, Password: pw}, nil
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
