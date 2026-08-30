package tools

import (
	"fmt"
	"net/url"
	"regexp"
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
	maxRegexLength   = 1024 // regex denylist rules: an alternation is easily longer than a name
)

// validateDomainName returns nil if s is acceptable as a Pi-hole denylist or
// allowlist rule of the given kind ("exact" or "regex"). The two kinds have
// almost nothing in common, and validating a regex as a DNS name rejected the
// most ordinary blocklist rules there are: an anchored suffix match needs '$'
// and an alternation needs '|', both of which the exact rules treat as shell
// metacharacters. Pi-hole accepts them.
//
// Anything that is not "regex" is validated as an exact name, so a missing or
// unexpected kind gets the stricter of the two checks.
func validateDomainName(s, kind string) error {
	if kind == "regex" {
		return validateDomainRegex(s)
	}
	return validateExactDomain(s)
}

// validateDomainRegex returns nil if s is a usable regular expression for a
// Pi-hole regex rule. Pi-hole compiles it server-side and rejects what it
// cannot parse, so this only catches the same class earlier and keeps out the
// characters that cannot travel in a URL path at all.
func validateDomainRegex(s string) error {
	if s == "" {
		return fmt.Errorf("must not be empty")
	}
	if len(s) > maxRegexLength {
		return fmt.Errorf("must be at most %d characters (got %d)", maxRegexLength, len(s))
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("must be valid UTF-8")
	}
	if strings.ContainsAny(s, "\t\r\n\x00") {
		return fmt.Errorf("must not contain control characters or NUL bytes")
	}
	// Go's regexp is RE2, which has no backreferences, and Pi-hole's engine
	// does: FTL v6.7 accepted ^(zzz)\1\.example\.com$ and rejected a
	// reference to a group that does not exist. Compiling here would refuse a
	// rule Pi-hole would have taken, with no way round it, so a pattern
	// carrying one is left for Pi-hole to judge. Pi-hole also rejects a few
	// things RE2 accepts, such as \p{L}, a named group, and a repeat count
	// over 255; all of those fail loudly at the API, which is the tolerable
	// direction for the two engines to disagree in.
	if hasBackreference(s) {
		return nil
	}
	if _, err := regexp.Compile(s); err != nil {
		return fmt.Errorf("is not a valid regular expression: %w", err)
	}
	return nil
}

// hasBackreference reports whether s contains a \1 to \9 escape. An escaped
// backslash consumes the character after it, so \\1 is a literal backslash
// followed by a digit rather than a reference.
func hasBackreference(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '\\' {
			continue
		}
		if s[i+1] >= '1' && s[i+1] <= '9' {
			return true
		}
		i++ // skip the escaped character
	}
	return false
}

// validateExactDomain returns nil if s looks like a syntactically valid DNS
// name as Pi-hole's exact denylists would accept it. We accept a leading "*."
// wildcard label as a single label for convenience because gravity-style
// entries often use it.
//
// The check is intentionally permissive: Pi-hole performs its own validation
// server-side, and rejecting too aggressively would surface false negatives
// for legitimate hostnames. We only reject inputs that cannot possibly
// represent a DNS name.
func validateExactDomain(s string) error {
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
