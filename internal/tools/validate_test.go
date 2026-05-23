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
			err := validateDomainName(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDomainName(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
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
