package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validToken is deliberately wordy rather than random: a high-entropy literal
// in a test file is what a leaked credential looks like to a secret scanner.
const validToken = "placeholder-token-not-real"

func setInstance(t *testing.T) {
	t.Helper()
	t.Setenv("PIHOLE_URL", "http://localhost:8081")
	t.Setenv("PIHOLE_PASSWORD", "test")
}

func TestLoad_HTTPAuthTokenUnsetByDefault(t *testing.T) {
	setInstance(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAuthToken != "" {
		t.Errorf("HTTPAuthToken = %q, want empty", cfg.HTTPAuthToken)
	}
	if cfg.TrustedProxies != nil {
		t.Errorf("TrustedProxies = %v, want nil", cfg.TrustedProxies)
	}
}

func TestLoad_HTTPAuthTokenFromEnv(t *testing.T) {
	setInstance(t)
	t.Setenv("PIHOLE_HTTP_AUTH_TOKEN", "  "+validToken+"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAuthToken != validToken {
		t.Errorf("HTTPAuthToken = %q, want %q (surrounding whitespace stripped)", cfg.HTTPAuthToken, validToken)
	}
}

func TestLoad_HTTPAuthTokenFromFile(t *testing.T) {
	setInstance(t)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(validToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIHOLE_HTTP_AUTH_TOKEN_FILE", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAuthToken != validToken {
		t.Errorf("HTTPAuthToken = %q, want %q", cfg.HTTPAuthToken, validToken)
	}
}

func TestLoad_HTTPAuthTokenRejections(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte("\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(t.TempDir(), "short")
	if err := os.WriteFile(short, []byte("tiny\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		env     map[string]string
		wantMsg string
	}{
		{
			name:    "both forms set",
			env:     map[string]string{"PIHOLE_HTTP_AUTH_TOKEN": validToken, "PIHOLE_HTTP_AUTH_TOKEN_FILE": missing},
			wantMsg: "not both",
		},
		{
			name:    "token too short to be worth having",
			env:     map[string]string{"PIHOLE_HTTP_AUTH_TOKEN": "hunter2"},
			wantMsg: "at least 16 characters",
		},
		{
			name:    "file cannot be read",
			env:     map[string]string{"PIHOLE_HTTP_AUTH_TOKEN_FILE": missing},
			wantMsg: "could not be read",
		},
		{
			name:    "file is empty",
			env:     map[string]string{"PIHOLE_HTTP_AUTH_TOKEN_FILE": empty},
			wantMsg: "is empty",
		},
		{
			name:    "file holds a token too short",
			env:     map[string]string{"PIHOLE_HTTP_AUTH_TOKEN_FILE": short},
			wantMsg: "at least 16 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setInstance(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatal("expected an error, got none")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantMsg)
			}
		})
	}
}

// TestLoad_HTTPAuthTokenEmptyValueIsUnset covers the misconfigured secret mount:
// an exported but empty variable must read as "no token", not as a token.
func TestLoad_HTTPAuthTokenEmptyValueIsUnset(t *testing.T) {
	setInstance(t)
	t.Setenv("PIHOLE_HTTP_AUTH_TOKEN", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAuthToken != "" {
		t.Errorf("HTTPAuthToken = %q, want empty", cfg.HTTPAuthToken)
	}
}

func TestLoad_TrustedProxies(t *testing.T) {
	setInstance(t)
	t.Setenv("PIHOLE_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.5 ,::1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.0/8", "192.168.1.5/32", "::1/128"}
	if len(cfg.TrustedProxies) != len(want) {
		t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	for i, w := range want {
		if got := cfg.TrustedProxies[i].String(); got != w {
			t.Errorf("TrustedProxies[%d] = %q, want %q", i, got, w)
		}
	}
}

func TestLoad_TrustedProxiesRejected(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantMsg string
	}{
		{"not an address", "not-an-ip", "not a valid IP address or CIDR block"},
		{"malformed CIDR", "10.0.0.0/64", "not a valid CIDR block"},
		{"hostname", "proxy.lan", "not a valid IP address or CIDR block"},
		{"only separators", ",, ,", "at least one IP address or CIDR block"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setInstance(t)
			t.Setenv("PIHOLE_TRUSTED_PROXIES", tt.value)
			_, err := Load()
			if err == nil {
				t.Fatal("expected an error, got none")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantMsg)
			}
		})
	}
}
