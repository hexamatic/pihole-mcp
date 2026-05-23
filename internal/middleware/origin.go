package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// wildcardOrigin disables Origin/Host enforcement. Documented as unsafe.
const wildcardOrigin = "*"

// OriginValidator implements DNS-rebinding protection for the MCP HTTP/SSE
// transport, per the 2025-11-25 spec recommendation. It enforces two rules:
//
//  1. If the request carries an Origin header, the host portion must be in
//     the allowlist; missing Origin is allowed (non-browser clients).
//  2. The Host header must be in the allowlist.
//
// Allowlist is built from a comma-separated env value. Default protects
// loopback only (localhost, 127.0.0.1, [::1]). The literal "*" disables
// enforcement entirely.
type OriginValidator struct {
	allowed map[string]struct{}
	wildAll bool
}

// NewOriginValidator constructs a validator from a list of allowed hosts.
// Each entry is matched case-insensitively against the host portion of the
// Origin URL and against the Host header. IPv6 literals should be supplied
// in bracketed form, e.g. "[::1]".
func NewOriginValidator(allowed []string) *OriginValidator {
	v := &OriginValidator{allowed: make(map[string]struct{})}
	for _, raw := range allowed {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if entry == wildcardOrigin {
			v.wildAll = true
			return v
		}
		v.allowed[strings.ToLower(entry)] = struct{}{}
	}
	return v
}

// Middleware wraps next with Origin and Host validation. When the validator
// has no entries it returns next unchanged (no-op).
func (v *OriginValidator) Middleware(next http.Handler) http.Handler {
	if v == nil || (len(v.allowed) == 0 && !v.wildAll) {
		return next
	}
	if v.wildAll {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			host, ok := hostOfOrigin(origin)
			if !ok || !v.hostAllowed(host) {
				rejectOrigin(w, "invalid Origin")
				return
			}
		}
		if !v.hostAllowed(stripPort(r.Host)) {
			rejectOrigin(w, "invalid Host")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (v *OriginValidator) hostAllowed(host string) bool {
	if v.wildAll {
		return true
	}
	h := strings.ToLower(host)
	if _, ok := v.allowed[h]; ok {
		return true
	}
	// IPv6 literals: caller may pass "[::1]" or "::1"; normalise both.
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		_, ok := v.allowed[h[1:len(h)-1]]
		return ok
	}
	bracketed := "[" + h + "]"
	if _, ok := v.allowed[bracketed]; ok {
		return true
	}
	return false
}

func hostOfOrigin(origin string) (string, bool) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return "", false
	}
	return stripPort(u.Host), true
}

func stripPort(host string) string {
	// Handles "host:port", "host", "[v6]:port", and "[v6]"
	if strings.HasPrefix(host, "[") {
		if idx := strings.LastIndex(host, "]"); idx != -1 {
			return host[:idx+1]
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func rejectOrigin(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":  "origin_blocked",
		"reason": reason,
	})
}
