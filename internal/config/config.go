// Package config loads Pi-hole MCP server configuration from environment variables.
package config

import (
	"fmt"
	"net/netip"
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

	// minAuthTokenLength is the shortest PIHOLE_HTTP_AUTH_TOKEN accepted. A
	// bearer token short enough to guess is worse than no token, because it
	// reads as protection that isn't there. Rejected at startup, where the
	// message can name the variable, rather than silently accepted.
	minAuthTokenLength = 16
)

// defaultAllowedOrigins is the loopback-only allowlist. Matches the
// DNS-rebinding protection prescribed by the MCP 2025-11-25 spec.
var defaultAllowedOrigins = []string{"localhost", "127.0.0.1", "[::1]"}

// Identity advertised to MCP clients during the protocol handshake. These mirror
// the matching fields of server.json, the manifest the MCP Registry publishes,
// so a client that installed us from the registry and a client talking to the
// running process see the same name, blurb and icons.
// TestServerJSONIdentityMatchesConstants fails if the two ever disagree.
const (
	ServerTitle       = "Pi-hole MCP Server"
	ServerDescription = "Manage Pi-hole v6: DNS blocking, domains, clients, query analysis, DHCP, and multi-instance sync."
	ServerWebsiteURL  = "https://github.com/hexamatic/pihole-mcp"
)

// ServerIcon is one entry of the icon set advertised to clients, matching the
// shape of an entry in server.json's "icons" array.
type ServerIcon struct {
	Src      string
	MIMEType string
	Sizes    []string
}

// ServerIcons is the icon set advertised over the protocol and in server.json.
// Raster images come first deliberately: the MCP specification tells clients to
// prefer PNG or JPEG and warns that an SVG may carry executable content, so the
// SVG is offered last as a resolution-independent fallback for clients that
// render it safely.
var ServerIcons = []ServerIcon{
	{Src: assetBaseURL + "logo-256.png", MIMEType: "image/png", Sizes: []string{"256x256"}},
	{Src: assetBaseURL + "logo-120.png", MIMEType: "image/png", Sizes: []string{"120x120"}},
	{Src: assetBaseURL + "logo.svg", MIMEType: "image/svg+xml", Sizes: []string{"any"}},
}

// assetBaseURL is where the icons above are served from. Icons have to be
// fetchable by a client that only has the binary, so they cannot be relative
// paths into the repository.
const assetBaseURL = "https://raw.githubusercontent.com/hexamatic/pihole-mcp/main/assets/"

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
	//
	// This is DNS-rebinding protection, not authentication: both headers are
	// supplied by the client, so any non-browser caller can set them freely.
	// HTTPAuthToken is the access control.
	AllowedOrigins []string

	// HTTPAuthToken is the shared bearer token the HTTP and SSE transports
	// require on every request. Empty (the default) leaves the transports
	// unauthenticated, which is only safe on a loopback bind.
	HTTPAuthToken string

	// TrustedProxies lists the networks whose X-Forwarded-For header the rate
	// limiter believes when working out which client a request came from.
	// Empty (the default) means the header is ignored entirely and the
	// immediate peer address is used.
	TrustedProxies []netip.Prefix

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

	token, err := loadAuthToken()
	if err != nil {
		return nil, err
	}
	cfg.HTTPAuthToken = token

	if v := os.Getenv("PIHOLE_TRUSTED_PROXIES"); v != "" {
		proxies, err := parseTrustedProxies(v)
		if err != nil {
			return nil, err
		}
		cfg.TrustedProxies = proxies
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
	u, err := url.Parse(rawURL)
	if err != nil {
		return InstanceConfig{}, fmt.Errorf("%s_URL is not a valid URL: %w", prefix, err)
	}
	// url.Parse accepts almost anything, so a bare host or a typo in the scheme
	// used to be carried all the way to the first tool call and surface there as
	// an opaque transport error. Reject it at startup, where the message can name
	// the variable.
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return InstanceConfig{}, fmt.Errorf("%s_URL must include a host, for example http://192.168.1.2 (got %q)", prefix, rawURL)
		}
	case "":
		return InstanceConfig{}, fmt.Errorf("%s_URL must start with http:// or https://, for example http://192.168.1.2 (got %q)", prefix, rawURL)
	default:
		return InstanceConfig{}, fmt.Errorf("%s_URL has scheme %q, but only http and https are supported. If that is a host and port, add the scheme: http://%s", prefix, u.Scheme, rawURL)
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

// loadAuthToken resolves the shared bearer token for the HTTP and SSE
// transports from PIHOLE_HTTP_AUTH_TOKEN or PIHOLE_HTTP_AUTH_TOKEN_FILE.
//
// The file form exists because an environment variable set on the command line
// is visible in `ps` to every user on the host, and container platforms mount
// secrets as files. Surrounding whitespace is stripped from both forms: a file
// written by `echo` ends in a newline, and a trailing space in an exported
// variable is never intentional.
//
// An empty value is treated as "not set" rather than "empty token", so a
// misconfigured secret mount cannot silently disable authentication while
// looking configured.
func loadAuthToken() (string, error) {
	inline, inlineSet := os.LookupEnv("PIHOLE_HTTP_AUTH_TOKEN")
	path, pathSet := os.LookupEnv("PIHOLE_HTTP_AUTH_TOKEN_FILE")
	inline, path = strings.TrimSpace(inline), strings.TrimSpace(path)
	inlineSet, pathSet = inlineSet && inline != "", pathSet && path != ""

	switch {
	case inlineSet && pathSet:
		return "", fmt.Errorf("set either PIHOLE_HTTP_AUTH_TOKEN or PIHOLE_HTTP_AUTH_TOKEN_FILE, not both")
	case pathSet:
		raw, err := os.ReadFile(path) //nolint:gosec // G304: reading the operator-nominated secret file is the point of this variable
		if err != nil {
			return "", fmt.Errorf("PIHOLE_HTTP_AUTH_TOKEN_FILE %q could not be read: %w", path, err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return "", fmt.Errorf("PIHOLE_HTTP_AUTH_TOKEN_FILE %q is empty; write the token to it or unset the variable", path)
		}
		return token, validateAuthToken(token, "PIHOLE_HTTP_AUTH_TOKEN_FILE "+path)
	case inlineSet:
		return inline, validateAuthToken(inline, "PIHOLE_HTTP_AUTH_TOKEN")
	default:
		return "", nil
	}
}

// validateAuthToken rejects a token short enough to be guessed. source names
// the variable or file the value came from so the message is actionable.
func validateAuthToken(token, source string) error {
	if len(token) < minAuthTokenLength {
		return fmt.Errorf("%s must be at least %d characters (got %d); generate one with: openssl rand -base64 32",
			source, minAuthTokenLength, len(token))
	}
	return nil
}

// parseTrustedProxies parses a comma-separated list of CIDR blocks and bare IP
// addresses into prefixes. A bare address becomes a single-host prefix.
//
// Nothing is trusted by default. X-Forwarded-For is client-supplied, so a
// server that believes it unconditionally lets any caller pick its own
// rate-limit bucket; the header is only consulted when the immediate peer is
// one of these.
func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	parts := strings.Split(raw, ",")
	out := make([]netip.Prefix, 0, len(parts))
	for _, p := range parts {
		entry := strings.TrimSpace(p)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("PIHOLE_TRUSTED_PROXIES entry %q is not a valid CIDR block: %w", entry, err)
			}
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("PIHOLE_TRUSTED_PROXIES entry %q is not a valid IP address or CIDR block (for example 10.0.0.0/8 or 192.168.1.5): %w", entry, err)
		}
		addr = addr.Unmap()
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("PIHOLE_TRUSTED_PROXIES must contain at least one IP address or CIDR block, or be unset")
	}
	return out, nil
}
