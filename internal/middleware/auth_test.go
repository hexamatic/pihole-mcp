package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "s3cret-token-long-enough"

func TestBearerAuth_DisabledWhenTokenEmpty(t *testing.T) {
	a := NewBearerAuth("")
	if a.Enabled() {
		t.Fatal("an empty token must not enable authentication")
	}

	rec := httptest.NewRecorder()
	a.Middleware(okHandler()).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want passthrough 200, got %d", rec.Code)
	}
}

func TestBearerAuth_Accepts(t *testing.T) {
	a := NewBearerAuth(testToken)
	if !a.Enabled() {
		t.Fatal("a configured token must enable authentication")
	}

	for _, header := range []string{"Bearer " + testToken, "bearer " + testToken, "BEARER  " + testToken} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		a.Middleware(okHandler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Authorization %q: want 200, got %d", header, rec.Code)
		}
	}
}

func TestBearerAuth_Rejects(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"no header at all", ""},
		{"wrong token", "Bearer not-the-token-at-all"},
		{"right token, wrong scheme", "Basic " + testToken},
		{"scheme only", "Bearer"},
		{"empty credential", "Bearer   "},
		{"token as a prefix of the real one", "Bearer " + testToken[:8]},
		{"token with the real one as a prefix", "Bearer " + testToken + "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			NewBearerAuth(testToken).Middleware(okHandler()).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
			}
			var body map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("response body is not JSON: %v", err)
			}
			if body["error"] != "unauthenticated" {
				t.Errorf(`error = %v, want "unauthenticated"`, body["error"])
			}
		})
	}
}

// TestBearerAuth_RejectsBeforeTheSessionLimiter is the ordering guarantee: an
// unauthenticated caller must not reach the MCP handler, and must not spend the
// session budget of a client that does hold the token.
func TestBearerAuth_RejectsBeforeTheSessionLimiter(t *testing.T) {
	rl := NewRateLimiter(60, 2)
	defer rl.Stop()

	reached := 0
	counted := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})
	failures := NewFailureLimiter()
	defer failures.Stop()
	h := Chain(NewBearerAuth(testToken, WithFailurePenalty(failures.Penalise)).Middleware, rl.Middleware)(counted)

	for i := 0; i < 50; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "203.0.113.30:9000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("request %d: want 401 or 429, got %d", i, rec.Code)
		}
	}
	if reached != 0 {
		t.Fatalf("%d unauthenticated requests reached the handler", reached)
	}

	rl.mu.Lock()
	tracked := len(rl.ips)
	rl.mu.Unlock()
	if tracked != 0 {
		t.Fatalf("rejected requests created %d entries in the request limiter; it ran before auth", tracked)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	// Same address as the flood: the token holder must still get through, which
	// is why the failure budget is separate from the request budget.
	req.RemoteAddr = "203.0.113.30:9000"
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("the token holder was locked out by rejected requests from the same address: got %d", rec.Code)
	}
}

// TestBearerAuth_BruteForceIsThrottled covers the cost of answering 401 ahead of
// the rate limiter: without a penalty on the reject path a token could be
// guessed as fast as the network allows, and nothing would say so.
func TestBearerAuth_BruteForceIsThrottled(t *testing.T) {
	failures := NewFailureLimiter()
	defer failures.Stop()
	h := NewBearerAuth(testToken, WithFailurePenalty(failures.Penalise)).Middleware(okHandler())

	var unauthorised, throttled int
	for i := 0; i < 500; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "203.0.113.40:9000"
		req.Header.Set("Authorization", "Bearer guess-number-"+itoa(i))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		switch rec.Code {
		case http.StatusUnauthorized:
			unauthorised++
		case http.StatusTooManyRequests:
			throttled++
		default:
			t.Fatalf("unexpected status %d", rec.Code)
		}
	}
	if throttled == 0 {
		t.Fatal("500 wrong tokens from one address were all answered at full rate")
	}
	if unauthorised == 0 {
		t.Fatal("expected the first attempts to be answered 401 rather than throttled")
	}
}

// TestBearerAuth_PenaltyNoOpWhenLimitingDisabled pins Penalise's behaviour on a
// disabled limiter. The shipped wiring uses NewFailureLimiter, which is never
// disabled, but a caller passing a disabled limiter must not get a 429 machine.
func TestBearerAuth_PenaltyNoOpWhenLimitingDisabled(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	defer rl.Stop()
	h := NewBearerAuth(testToken, WithFailurePenalty(rl.Penalise)).Middleware(okHandler())

	for i := 0; i < 100; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: want 401, got %d", i, rec.Code)
		}
	}
}
