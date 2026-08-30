package tools

import (
	"strings"
	"testing"
)

func TestValidateDomainName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"simple", "example.com", false},
		{"subdomain", "ads.example.com", false},
		{"wildcard label", "*.example.com", false},
		{"single label", "localhost", false},
		{"trailing dot", "example.com.", true}, // empty trailing label
		{"empty", "", true},
		{"whitespace", "example .com", true},
		{"newline", "example\n.com", true},
		{"NUL byte", "exa\x00mple.com", true},
		{"single quote injection", "example'.com", true},
		{"double quote injection", "exa\"mple.com", true},
		{"semicolon", "example.com;", true},
		{"DROP TABLE attempt", "'; DROP TABLE--", true},
		{"pipe", "example|.com", true},
		{"backtick", "exa`mple.com", true},
		{"shell var", "$example.com", true},
		{"too long", strings.Repeat("a", 254), true},
		{"label too long", strings.Repeat("a", 64) + ".com", true},
		{"max length ok", strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." + strings.Repeat("c", 60) + ".com", false},
		{"unicode allowed", "münchen.example.de", false},
		{"invalid utf8", string([]byte{0xff, 0xfe, 0xfd}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDomainName(tt.in, "exact")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDomainName(%q, exact) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"https with host", "https://raw.githubusercontent.com/foo/bar.txt", false},
		{"http with host", "http://example.com/list.txt", false},
		{"file with path", "file:///etc/pihole/local.list", false},
		{"empty", "", true},
		{"scheme only", "http://", true},
		{"no scheme", "example.com/list.txt", true},
		{"unsupported scheme", "ftp://example.com/list.txt", true},
		{"file with no path", "file://", true},
		{"contains whitespace", "https://example.com /list.txt", true},
		{"newline", "https://example.com/list\n.txt", true},
		{"unicode allowed", "https://例え.example.com/list.txt", false},
		{"NUL byte", "https://example.com/\x00list.txt", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateURL(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestValidateMaxLength(t *testing.T) {
	if err := validateMaxLength("comment", "short", 1024); err != nil {
		t.Errorf("short string should pass: %v", err)
	}
	if err := validateMaxLength("comment", strings.Repeat("a", 1024), 1024); err != nil {
		t.Errorf("exactly max length should pass: %v", err)
	}
	if err := validateMaxLength("comment", strings.Repeat("a", 1025), 1024); err == nil {
		t.Error("over-limit string should fail")
	}
	// Multi-byte runes count as one rune each.
	if err := validateMaxLength("name", strings.Repeat("μ", 255), 255); err != nil {
		t.Errorf("255 multi-byte runes within limit should pass: %v", err)
	}
	if err := validateMaxLength("name", string([]byte{0xff}), 1024); err == nil {
		t.Error("invalid UTF-8 should fail")
	}
}

func TestValidateIntRange(t *testing.T) {
	if err := validateIntRange("count", 5, 1, 100); err != nil {
		t.Errorf("in-range should pass: %v", err)
	}
	if err := validateIntRange("count", 0, 1, 100); err == nil {
		t.Error("below range should fail")
	}
	if err := validateIntRange("count", 101, 1, 100); err == nil {
		t.Error("above range should fail")
	}
	if err := validateIntRange("count", 1, 1, 100); err != nil {
		t.Errorf("min boundary should pass: %v", err)
	}
	if err := validateIntRange("count", 100, 1, 100); err != nil {
		t.Errorf("max boundary should pass: %v", err)
	}
}

// A regex rule is not a DNS name, and validating it as one rejected the most
// ordinary blocklist entries there are: an anchored suffix match needs '$',
// an alternation needs '|', and both were on the shell-metacharacter list.
// Pi-hole accepts them; the tool would not let them through.
func TestValidateDomainName_Regex(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"anchored suffix", `.*\.doubleclick\.net$`, false},
		{"alternation", `^(ads|track)\.example\.com$`, false},
		{"quantifier with a comma", `^ads{1,3}\.example\.com$`, false},
		{"character class and plus", `^ads[0-9]+\.example\.com`, false},
		{"dollar and pipe together", `(^|\.)doubleclick\.net$`, false},
		// Go's regexp is RE2, which has no backreferences, but Pi-hole's engine
		// does: FTL v6.7 accepts ^(zzz)\1\.example\.com$ and rejects a
		// reference to a group that does not exist. Refusing these here would
		// block a rule Pi-hole would have taken, with no way round it, so a
		// pattern carrying one is left for Pi-hole to judge. The cost is that a
		// malformed pattern containing one fails at the API instead of here.
		{"backreference", `^(zzz)\1\.example\.com$`, false},
		{"malformed pattern carrying a backreference", `^(zzz\1`, false},
		{"empty", "", true},
		{"unclosed group", `^(ads`, true},
		{"unclosed class", `^ads[0-9`, true},
		{"NUL byte", "ads\x00", true},
		{"newline", "^ads\n.com", true},
		{"invalid utf8", string([]byte{0xff, 0xfe, 0xfd}), true},
		{"too long", strings.Repeat("a", maxRegexLength+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDomainName(tt.in, "regex")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDomainName(%q, regex) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

// The exact rules still apply to an exact rule: a regex that would sail
// through the regex branch must not sail through this one.
func TestValidateDomainName_ExactStillRejectsRegexSyntax(t *testing.T) {
	for _, in := range []string{`.*\.doubleclick\.net$`, `^(ads|track)\.example\.com$`} {
		if err := validateDomainName(in, "exact"); err == nil {
			t.Errorf("validateDomainName(%q, exact) = nil, want an error", in)
		}
	}
}
