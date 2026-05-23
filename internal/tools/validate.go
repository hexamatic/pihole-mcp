package tools

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Length caps for user-supplied free-form strings. Sized to match upstream
// Pi-hole/DNS expectations rather than guesses — keep them in sync with the
// API server's own limits if those change.
const (
	maxDomainLength  = 253  // RFC 1035: total length of a domain name in dotted form
	maxLabelLength   = 63   // RFC 1035: per-label length
	maxCommentLength = 1024 // Pi-hole-side comment field is "TEXT" but kept sane
	maxNameLength    = 255  // generic free-form name (groups, etc.)
	maxConfigPathLen = 256  // dotted config paths like dns.upstreams
)

// validateDomainName returns nil if s looks like a syntactically valid DNS
// name as Pi-hole's exact/regex denylists would accept it. We accept a
// leading "*." wildcard label as a single label for convenience because
// gravity-style entries often use it.
//
// The check is intentionally permissive: Pi-hole performs its own validation
// server-side, and rejecting too aggressively would surface false negatives
// for legitimate hostnames. We only reject inputs that cannot possibly
// represent a DNS name.
func validateDomainName(s string) error {
	if s == "" {
		return fmt.Errorf("must not be empty")
	}
	if len(s) > maxDomainLength {
		return fmt.Errorf("must be at most %d characters (got %d)", maxDomainLength, len(s))
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("must be valid UTF-8")
	}
	if strings.ContainsAny(s, " \t\r\n\x00") {
		return fmt.Errorf("must not contain whitespace or NUL bytes")
	}
	// Reject quote and shell metacharacters that have no business being in a
	// DNS name — defends against accidental command-injection-shaped payloads
	// being forwarded to the Pi-hole API.
	if strings.ContainsAny(s, "'\";`$<>|&") {
		return fmt.Errorf("must not contain shell metacharacters")
	}

	labels := strings.Split(s, ".")
	for i, label := range labels {
		// Allow a leading "*" wildcard label as a single character.
		if i == 0 && label == "*" {
			continue
		}
		if label == "" {
			return fmt.Errorf("must not contain empty labels (consecutive dots)")
		}
		if len(label) > maxLabelLength {
			return fmt.Errorf("label %q exceeds %d characters", label, maxLabelLength)
		}
	}
	return nil
}

// validateURL returns nil if s parses as an HTTP/HTTPS or file URL with a
// non-empty host or path. Pi-hole list URLs accept http, https, and file
// schemes; anything else is almost always a typo.
func validateURL(s string) error {
	if s == "" {
		return fmt.Errorf("must not be empty")
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("must be valid UTF-8")
	}
	if strings.ContainsAny(s, " \t\r\n\x00") {
		return fmt.Errorf("must not contain whitespace or NUL bytes")
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return fmt.Errorf("http(s) URLs must include a host")
		}
	case "file":
		if u.Path == "" {
			return fmt.Errorf("file URLs must include a path")
		}
	case "":
		return fmt.Errorf("must include a scheme (http, https, or file)")
	default:
		return fmt.Errorf("unsupported scheme %q (expected http, https, or file)", u.Scheme)
	}
	return nil
}

// validateMaxLength returns nil if len(s) <= maxRunes (counted in UTF-8
// runes, not bytes). name is the human-readable field name surfaced in the
// error message.
func validateMaxLength(name, s string, maxRunes int) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s must be valid UTF-8", name)
	}
	n := utf8.RuneCountInString(s)
	if n > maxRunes {
		return fmt.Errorf("%s must be at most %d characters (got %d)", name, maxRunes, n)
	}
	return nil
}

// validateIntRange returns nil if v is within [minVal, maxVal] inclusive.
func validateIntRange(name string, v, minVal, maxVal int) error {
	if v < minVal || v > maxVal {
		return fmt.Errorf("%s must be between %d and %d (got %d)", name, minVal, maxVal, v)
	}
	return nil
}
