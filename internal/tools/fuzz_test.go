package tools

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzValidateDomainName asserts the safety properties the rest of the
// codebase relies on: the validator never panics, and any input it accepts
// is valid UTF-8, within RFC 1035 length limits, and free of whitespace,
// NUL bytes, and shell metacharacters.
func FuzzValidateDomainName(f *testing.F) {
	seeds := []string{
		"",
		"example.com",
		"*.example.com",
		"sub.domain.example.co.uk",
		"xn--nxasmq6b.example",
		"a..b",
		"*.",
		".",
		"ads.example.com\x00",
		"evil.com; rm -rf /",
		"$(whoami).example.com",
		"domain with spaces.com",
		"héllo.example.com",
		"\xff\xfe.example.com",
		strings.Repeat("a", 254),
		strings.Repeat("a", 64) + ".example.com",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if err := validateDomainName(s, "exact"); err != nil {
			return
		}
		// Accepted input must uphold every documented invariant.
		if s == "" {
			t.Error("accepted empty string")
		}
		if len(s) > maxDomainLength {
			t.Errorf("accepted %d bytes, cap is %d", len(s), maxDomainLength)
		}
		if !utf8.ValidString(s) {
			t.Errorf("accepted invalid UTF-8: %q", s)
		}
		if strings.ContainsAny(s, " \t\r\n\x00") {
			t.Errorf("accepted whitespace/NUL: %q", s)
		}
		if strings.ContainsAny(s, "'\";`$<>|&") {
			t.Errorf("accepted shell metacharacters: %q", s)
		}
		for i, label := range strings.Split(s, ".") {
			if i == 0 && label == "*" {
				continue
			}
			if label == "" {
				t.Errorf("accepted empty label: %q", s)
			}
			if len(label) > maxLabelLength {
				t.Errorf("accepted %d-byte label, cap is %d: %q", len(label), maxLabelLength, s)
			}
		}
	})
}

// FuzzValidateURL asserts the safety properties every caller of validateURL
// relies on. Blocklist URLs are the one tool parameter Pi-hole will later
// fetch over the network on our behalf, so an accepted value that is not what
// it appears to be is a request forged through us.
//
// The invariants checked are deliberately the ones the validator states rather
// than the ones url.Parse happens to enforce, because url.Parse is famously
// permissive: it accepts a bare "//host", a scheme it has never heard of, and
// a string with a NUL byte in it. Anything this target accepts must round-trip
// through url.Parse, carry one of the three supported schemes, be free of
// whitespace and NUL bytes, and have the component that scheme requires.
func FuzzValidateURL(f *testing.F) {
	seeds := []string{
		"",
		"https://example.com/list.txt",
		"http://192.168.1.1/blocklist",
		"file:///etc/pihole/list.txt",
		"HTTPS://EXAMPLE.COM/List.TXT",
		"ftp://example.com/list.txt",
		"example.com/list.txt",
		"https://",
		"file://",
		"//example.com/list.txt",
		"https://example.com/list.txt\x00",
		"https://example.com/a b",
		"https://exa\tmple.com",
		"javascript:alert(1)",
		"https://\xff\xfe.example.com",
		"https://user:pass@example.com/list.txt",
		"https://[::1]:8080/list.txt",
		strings.Repeat("https://a", 200),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if err := validateURL(s); err != nil {
			return
		}
		// Accepted input must uphold every documented invariant.
		if s == "" {
			t.Error("accepted empty string")
		}
		if !utf8.ValidString(s) {
			t.Errorf("accepted invalid UTF-8: %q", s)
		}
		if strings.ContainsAny(s, " \t\r\n\x00") {
			t.Errorf("accepted whitespace/NUL: %q", s)
		}
		u, err := url.Parse(s)
		if err != nil {
			t.Fatalf("accepted a string url.Parse rejects: %q (%v)", s, err)
		}
		switch scheme := strings.ToLower(u.Scheme); scheme {
		case "http", "https":
			if u.Host == "" {
				t.Errorf("accepted a hostless %s URL: %q", scheme, s)
			}
		case "file":
			if u.Path == "" {
				t.Errorf("accepted a pathless file URL: %q", s)
			}
		default:
			t.Errorf("accepted unsupported scheme %q: %q", scheme, s)
		}
	})
}
