package tools

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestToolError_AuthError(t *testing.T) {
	err := &pihole.AuthError{APIError: &pihole.APIError{StatusCode: 401, Message: "Unauthorized"}}
	result := toolError("get stats", err)
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	text := mustText(t, result)
	if !strings.Contains(text, "Failed to get stats") {
		t.Errorf("expected action in message, got: %s", text)
	}
	if !strings.Contains(text, "Check PIHOLE_PASSWORD") {
		t.Errorf("expected auth guidance in message, got: %s", text)
	}
}

func TestToolError_RateLimitError(t *testing.T) {
	err := &pihole.RateLimitError{APIError: &pihole.APIError{StatusCode: 429, Message: "Rate limit exceeded"}}
	result := toolError("list domains", err)
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	text := mustText(t, result)
	if !strings.Contains(text, "Failed to list domains") {
		t.Errorf("expected action in message, got: %s", text)
	}
	if !strings.Contains(text, "try again shortly") {
		t.Errorf("expected rate limit guidance in message, got: %s", text)
	}
}

func TestToolError_NotFoundError(t *testing.T) {
	err := &pihole.NotFoundError{APIError: &pihole.APIError{StatusCode: 404, Message: "Not found"}}
	result := toolError("get client", err)
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	text := mustText(t, result)
	if !strings.Contains(text, "Failed to get client") {
		t.Errorf("expected action in message, got: %s", text)
	}
	if !strings.Contains(text, "does not exist") {
		t.Errorf("expected not-found guidance in message, got: %s", text)
	}
}

func TestToolError_ValidationError(t *testing.T) {
	err := &pihole.ValidationError{APIError: &pihole.APIError{StatusCode: 400, Message: "Invalid domain format"}}
	result := toolError("add domain", err)
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	text := mustText(t, result)
	if !strings.Contains(text, "Failed to add domain") {
		t.Errorf("expected action in message, got: %s", text)
	}
	if !strings.Contains(text, "Invalid domain format") {
		t.Errorf("expected validation message, got: %s", text)
	}
}

func TestToolError_ValidationError_WithHint(t *testing.T) {
	err := &pihole.ValidationError{APIError: &pihole.APIError{
		StatusCode: 400,
		Message:    "Invalid domain format",
		Hint:       "Use a FQDN like example.com",
	}}
	result := toolError("add domain", err)
	text := mustText(t, result)
	if !strings.Contains(text, "Use a FQDN like example.com") {
		t.Errorf("expected hint in message, got: %s", text)
	}
}

func TestToolError_APIError(t *testing.T) {
	err := &pihole.APIError{StatusCode: 500, Message: "Internal server error"}
	result := toolError("restart DNS", err)
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	text := mustText(t, result)
	if !strings.Contains(text, "Failed to restart DNS") {
		t.Errorf("expected action in message, got: %s", text)
	}
	if !strings.Contains(text, "Internal server error") {
		t.Errorf("expected API error message, got: %s", text)
	}
}

func TestToolError_APIError_WithHint(t *testing.T) {
	err := &pihole.APIError{StatusCode: 500, Message: "Database locked", Hint: "Wait and retry"}
	result := toolError("get stats", err)
	text := mustText(t, result)
	if !strings.Contains(text, "Wait and retry") {
		t.Errorf("expected hint in message, got: %s", text)
	}
}

func TestToolError_GenericError(t *testing.T) {
	err := fmt.Errorf("connection refused")
	result := toolError("get stats", err)
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	text := mustText(t, result)
	if !strings.Contains(text, "Failed to get stats") {
		t.Errorf("expected action in message, got: %s", text)
	}
	if !strings.Contains(text, "connection refused") {
		t.Errorf("expected original error in message, got: %s", text)
	}
}

// mustText extracts the text content from a CallToolResult for test assertions.
func mustText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// TestToolErrorTransportHints covers the four connection-level failures that
// never reach the Pi-hole API, so never arrive as a typed API error.
//
// Each one used to render as a bare Go error. The model calling the tool sees
// only this string, and every one of these has a fix the README already
// documents, so the model was being asked to debug a network problem from
// "dial tcp 192.168.1.2:80: connect: connection refused".
func TestToolErrorTransportHints(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "untrusted certificate",
			err:  fmt.Errorf("pi-hole API request failed: %w", &url.Error{Op: "Get", URL: "https://192.168.1.2/api/stats/summary", Err: x509.UnknownAuthorityError{}}),
			want: "PIHOLE_TLS_SKIP_VERIFY=true",
		},
		{
			name: "connection refused",
			err:  fmt.Errorf("pi-hole API request failed: %w", &url.Error{Op: "Get", URL: "http://192.168.1.2/api/stats/summary", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}),
			want: "Nothing is listening",
		},
		{
			name: "name does not resolve",
			err:  fmt.Errorf("pi-hole API request failed: %w", &url.Error{Op: "Get", URL: "http://pihole.invalid/api", Err: &net.DNSError{Err: "no such host", Name: "pihole.invalid", IsNotFound: true}}),
			want: "did not resolve",
		},
		{
			name: "missing scheme",
			err:  fmt.Errorf("pi-hole API request failed: %w", &url.Error{Op: "Get", URL: "192.168.1.2/api", Err: errors.New(`unsupported protocol scheme ""`)}),
			want: "must start with http:// or https://",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := toolError("get stats", tc.err)
			if !result.IsError {
				t.Fatal("expected IsError to be true")
			}
			text := mustText(t, result)
			if !strings.Contains(text, tc.want) {
				t.Errorf("error gives the caller no way to fix this (want %q):\n%s", tc.want, text)
			}
			if !strings.Contains(text, "Failed to get stats") {
				t.Errorf("error dropped the action: %s", text)
			}
		})
	}
}

// TestToolErrorLeavesOtherErrorsAlone checks the hints do not fire on errors
// they do not explain, which would attach a misleading fix to an unrelated
// failure.
func TestToolErrorLeavesOtherErrorsAlone(t *testing.T) {
	text := mustText(t, toolError("get stats", errors.New("unmarshalling response from /stats/summary: unexpected end of JSON input")))
	for _, unwanted := range []string{"PIHOLE_TLS_SKIP_VERIFY", "Nothing is listening", "did not resolve", "must start with http"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("unrelated error picked up the %q hint: %s", unwanted, text)
		}
	}
}
