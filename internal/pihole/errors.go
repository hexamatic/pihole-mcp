// Package pihole provides an HTTP client for the Pi-hole v6 REST API
// with transparent session-based authentication.
package pihole

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// seatsExceededKey is the error key Pi-hole returns when its concurrent API
// session pool is full. Confirmed against FTL v6.7.
const seatsExceededKey = "api_seats_exceeded"

// APIError represents an error response from the Pi-hole API.
type APIError struct {
	StatusCode int    // HTTP status code
	Key        string // Machine-readable error type (e.g. "bad_request")
	Message    string // Human-readable error message
	Hint       string // Additional context (may be empty)
	Endpoint   string // The API path that failed

	// RetryAfter is the delay requested by a Retry-After response header.
	// Pi-hole does not currently send one on any 429, but honouring it costs
	// nothing and means we follow the server the day it starts.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("pi-hole API error %d on %s: %s", e.StatusCode, e.Endpoint, e.Message)
	if e.Hint != "" {
		s += " (hint: " + e.Hint + ")"
	}
	return s
}

// AuthError is returned when the Pi-hole API responds with 401 Unauthorized.
type AuthError struct{ *APIError }

// NotFoundError is returned when a requested resource does not exist (404).
type NotFoundError struct{ *APIError }

// ValidationError is returned for invalid request parameters (400).
type ValidationError struct{ *APIError }

// ForbiddenError is returned when Pi-hole refuses an authenticated request
// (403). The common case is a session opened with an application password,
// which FTL lets read but not change the configuration, export a backup
// (FTL v6.8+) or import one unless webserver.api.app_sudo is true.
type ForbiddenError struct{ *APIError }

// AppSudoRequired reports whether Pi-hole refused because the session uses an
// application password and webserver.api.app_sudo is off. FTL names the
// setting in the hint of every such refusal, in v6.7 and v6.8 alike.
func (e *ForbiddenError) AppSudoRequired() bool {
	return strings.Contains(e.Hint, "app_sudo") || strings.Contains(e.Message, "app_sudo")
}

// RateLimitError is returned for a 429 that is expected to clear on its own.
// In practice this is FTL's failed-login brute-force limiter, which decays
// within seconds, so it is safe to retry.
type RateLimitError struct{ *APIError }

// SessionLimitError is returned when Pi-hole's concurrent API session pool is
// full — a 429 carrying the "api_seats_exceeded" key.
//
// This is emphatically not a rate limit. A seat is only released when a session
// times out (webserver.session.timeout, default 30 minutes) or is explicitly
// revoked, so retrying cannot succeed within any useful window. It is never
// retried, and the message tells the user how to fix it themselves — this is
// the most frequently reported Pi-hole v6 API problem.
type SessionLimitError struct{ *APIError }

func (e *SessionLimitError) Error() string {
	return "pi-hole rejected the login: its API session pool is full. " +
		"Pi-hole allows a limited number of concurrent API sessions (webserver.api.max_sessions, default 16) " +
		"and every client that logs in — the web interface, PADD, other integrations — takes a seat. " +
		"To fix: raise webserver.api.max_sessions, or free a seat by listing sessions with the " +
		"pihole_auth_sessions tool and revoking an idle one with pihole_auth_revoke_session. " +
		"Seats also release themselves once webserver.session.timeout elapses (default 30 minutes)."
}

// loginError marks a failure that happened while establishing a session, rather
// than while performing the caller's actual request.
//
// The distinction matters for retries: the caller's request never left the
// building, so replaying it cannot double-apply anything — even when the caller
// was mid-way through a POST or a DELETE.
type loginError struct{ err error }

func (e *loginError) Error() string { return e.err.Error() }
func (e *loginError) Unwrap() error { return e.err }

// classifyError wraps an APIError into the appropriate typed error.
func classifyError(e *APIError) error {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return &AuthError{e}
	case http.StatusNotFound:
		return &NotFoundError{e}
	case http.StatusBadRequest:
		return &ValidationError{e}
	case http.StatusForbidden:
		return &ForbiddenError{e}
	case http.StatusTooManyRequests:
		// Both conditions are 429; only the key tells them apart, and they need
		// opposite handling — one is retryable, the other is not.
		if e.Key == seatsExceededKey {
			return &SessionLimitError{e}
		}
		return &RateLimitError{e}
	default:
		return e
	}
}

// parseRetryAfter reads a Retry-After header in either of its RFC 9110 forms
// (delay-seconds or an HTTP-date). Returns 0 when absent or unparseable.
func parseRetryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// UncertainError marks a change request that was sent but got no reply: the
// connection dropped or timed out after the request went out. Pi-hole may or
// may not have applied it, so the caller must check before trying again.
//
// The usual cause is a write landing just as FTL restarts to apply an earlier
// change. FTL applies the write and then exits before answering, the caller
// sees EOF, and a retry fails with "already present", which reads as though
// the first attempt had done nothing.
type UncertainError struct{ Err error }

func (e *UncertainError) Error() string { return e.Err.Error() }
func (e *UncertainError) Unwrap() error { return e.Err }

// mayHaveApplied reports whether err leaves it unknown whether a sent request
// was processed: the connection ended or timed out with the request already
// sent. A refused connection or a failed login means nothing was sent.
//
// live says whether the request's context was still live when the request
// began. A context that ends while the request is out, such as a client
// cancelling the tool call, abandons the reply but not the change, so that
// outcome is uncertain too. A context that had already ended stopped the
// request before anything was sent.
func mayHaveApplied(err error, live bool) bool {
	if err == nil {
		return false
	}
	var le *loginError
	if errors.As(err, &le) || errors.Is(err, syscall.ECONNREFUSED) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return live
	}
	switch {
	case errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.EPIPE):
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
