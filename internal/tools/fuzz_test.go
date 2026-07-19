package tools

import (
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
		if err := validateDomainName(s); err != nil {
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
