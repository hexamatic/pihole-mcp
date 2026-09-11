package tools

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
)

// toolError creates an actionable MCP tool error from a Pi-hole API error.
// It inspects the error type and adds contextual guidance for tool clients
// to understand what went wrong and how to fix it.
func toolError(action string, err error) *mcp.CallToolResult {
	msg := fmt.Sprintf("Failed to %s", action)

	var authErr *pihole.AuthError
	var notFoundErr *pihole.NotFoundError
	var validationErr *pihole.ValidationError
	var rateLimitErr *pihole.RateLimitError
	var apiErr *pihole.APIError

	switch {
	case errors.As(err, &authErr):
		msg += fmt.Sprintf(": %s. Check PIHOLE_PASSWORD or use an application password.", authErr.Message)
	case errors.As(err, &rateLimitErr):
		msg += fmt.Sprintf(": %s. Too many requests — try again shortly.", rateLimitErr.Message)
	case errors.As(err, &notFoundErr):
		msg += fmt.Sprintf(": %s. The requested resource does not exist.", notFoundErr.Message)
	case errors.As(err, &validationErr):
		msg += fmt.Sprintf(": %s", validationErr.Message)
		if validationErr.Hint != "" {
			msg += " (" + validationErr.Hint + ")"
		}
	case errors.As(err, &apiErr):
		msg += fmt.Sprintf(": %s", apiErr.Message)
		if apiErr.Hint != "" {
			msg += " (" + apiErr.Hint + ")"
		}
	default:
		msg += fmt.Sprintf(": %v", err)
		if hint := TransportHint(err); hint != "" {
			msg += " " + hint
		}
	}

	return mcp.NewToolResultError(msg)
}

// TransportHint returns the fix for a connection-level failure, or "" when the
// error is not one this recognises.
//
// These four never reach the Pi-hole API at all, so none of them arrives as a
// typed API error and all four used to render as a bare Go error such as
// "dial tcp 192.168.1.2:80: connect: connection refused". The README explains
// each one, but the model calling the tool does not read the README: it sees
// only this string, and without the fix in it the best it can do is retry.
//
// Exported because -check reports the same failures to a human, and the two
// surfaces giving different advice for the same error is how they drift.
func TransportHint(err error) string {
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var certInvalid x509.CertificateInvalidError

	switch {
	case errors.As(err, &unknownAuthority), errors.As(err, &hostname), errors.As(err, &certInvalid):
		return "The Pi-hole is serving HTTPS with a certificate this machine does not trust. " +
			"Install a trusted certificate on the Pi-hole, or set PIHOLE_TLS_SKIP_VERIFY=true to accept it unverified " +
			"(still encrypted, but the server's identity is no longer checked, so only on a network you control)."

	case errors.Is(err, syscall.ECONNREFUSED):
		return "Nothing is listening on that address and port. Check PIHOLE_URL, including the port, " +
			"and that the Pi-hole web interface is running. Inside a container, localhost is the container: " +
			"use the host's LAN address, host.docker.internal on Docker Desktop, or the Pi-hole container's name on a shared network."

	case isDNSError(err):
		return "The hostname in PIHOLE_URL did not resolve. Check the spelling, or use the Pi-hole's IP address instead."

	case isMissingScheme(err):
		return "PIHOLE_URL is missing its scheme. It must start with http:// or https://, for example http://192.168.1.2."
	}
	return ""
}

func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// isMissingScheme reports whether err is net/http refusing a URL with no
// scheme. The transport builds that error with errors.New, so there is no type
// to match on and the message is the only handle available.
func isMissingScheme(err error) bool {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return false
	}
	return strings.Contains(urlErr.Error(), `unsupported protocol scheme ""`)
}
