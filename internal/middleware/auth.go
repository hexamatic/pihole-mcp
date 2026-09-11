package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// bearerScheme is the Authorization scheme accepted, matched case-insensitively
// per RFC 7235.
const bearerScheme = "bearer "

// BearerAuth requires a shared secret on every HTTP/SSE request.
//
// This is the transports' only access control. Origin and Host validation
// (OriginValidator) is DNS-rebinding protection for browsers and cannot
// authenticate anything: both headers are chosen by the client, so any caller
// that is not a browser sets them to whatever the allowlist wants.
//
// A single shared token rather than OAuth is a deliberate scope choice. The
// deployment this protects is one user pointing one MCP client at a server on
// their own network, and a token they can generate with openssl is the control
// that actually gets turned on.
type BearerAuth struct {
	// penalise charges a rejected request to something, so that a wrong token
	// is not free. Nil means rejections cost nothing.
	penalise func(*http.Request) bool

	// want is the SHA-256 of the configured token. Digests are compared rather
	// than the tokens themselves so the comparison is over two fixed-length
	// values: subtle.ConstantTimeCompare returns early when lengths differ, so
	// comparing raw tokens would leak the token's length through timing.
	want [sha256.Size]byte
}

// AuthOption configures a BearerAuth.
type AuthOption func(*BearerAuth)

// WithFailurePenalty charges every rejected request to charge, which reports
// whether the sender is still under its limit. Pass RateLimiter.Penalise: it
// makes a wrong token cost the sender ceiling budget even though the 401 is
// answered before the rate limiter runs, so a token cannot be guessed at line
// rate.
func WithFailurePenalty(charge func(*http.Request) bool) AuthOption {
	return func(a *BearerAuth) { a.penalise = charge }
}

// NewBearerAuth returns a validator for the given token, or nil when the token
// is empty. A nil *BearerAuth is a working no-op, so callers do not need to
// branch on whether authentication is configured.
func NewBearerAuth(token string, opts ...AuthOption) *BearerAuth {
	if token == "" {
		return nil
	}
	a := &BearerAuth{want: sha256.Sum256([]byte(token))}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Enabled reports whether a token is configured.
func (a *BearerAuth) Enabled() bool { return a != nil }

// Middleware wraps next with bearer token enforcement, answering 401 for a
// missing or wrong token. A nil receiver returns next unchanged.
//
// This runs before the rate limiter: an unauthenticated caller must not be able
// to consume another client's rate-limit budget, and the comparison is one
// SHA-256 over a header value, which is cheaper than the bucket lookup it
// precedes.
func (a *BearerAuth) Middleware(next http.Handler) http.Handler {
	if a == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			a.reject(w, r, "missing bearer token in the Authorization header")
			return
		}
		if !a.matches(presented) {
			a.reject(w, r, "invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// reject answers a request that failed authentication, charging it to the
// failure penalty first. A sender that has exhausted its budget is told it is
// being throttled instead, which is what stops an unlimited guessing rate.
func (a *BearerAuth) reject(w http.ResponseWriter, r *http.Request, reason string) {
	if a.penalise != nil && !a.penalise(r) {
		rejectRateLimited(w)
		return
	}
	rejectUnauthenticated(w, reason)
}

// matches reports whether presented is the configured token, in time that does
// not depend on how much of the token is correct.
func (a *BearerAuth) matches(presented string) bool {
	got := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(got[:], a.want[:]) == 1
}

// bearerToken extracts the credential from an Authorization header value.
func bearerToken(header string) (string, bool) {
	if len(header) < len(bearerScheme) || !strings.EqualFold(header[:len(bearerScheme)], bearerScheme) {
		return "", false
	}
	token := strings.TrimSpace(header[len(bearerScheme):])
	if token == "" {
		return "", false
	}
	return token, true
}

func rejectUnauthenticated(w http.ResponseWriter, reason string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="pihole-mcp"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":  "unauthenticated",
		"reason": reason,
	})
}
