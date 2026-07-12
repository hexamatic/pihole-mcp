// Package pihole provides an HTTP client for the Pi-hole v6 REST API
// with transparent session-based authentication.
package pihole

import (
	"fmt"
	"net/http"
	"strconv"
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
