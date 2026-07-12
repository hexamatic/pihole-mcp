package pihole

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetry keeps the backoff out of the test's wall clock. The delay schedule
// itself is covered by TestBackoffDelay.
func fastRetry(maxRetries int) Option {
	return WithRetry(maxRetries, time.Millisecond)
}

// errorBody writes a Pi-hole-shaped error response.
func errorBody(w http.ResponseWriter, status int, key, message, hint string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"key":"` + key + `","message":"` + message + `","hint":"` + hint + `"}}`))
}

// authOK answers /api/auth with a valid session. Returns true if it handled r.
func authOK(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/auth" {
		writeJSON(w, authResponse{Session: sessionInfo{Valid: true, SID: "sid"}})
		return true
	}
	return false
}

func TestRetry_TransientEOFOnGET_Recovers(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK(w, r) {
			return
		}
		// Hang up mid-response twice, exactly as FTL's web server does under
		// load, then answer normally.
		if attempts.Add(1) <= 2 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("ResponseWriter is not a Hijacker")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_ = conn.Close()
			return
		}
		writeJSON(w, BlockingStatus{Blocking: "enabled"})
	}))
	defer srv.Close()

	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), fastRetry(3))

	var status BlockingStatus
	if err := c.Get(context.Background(), "/dns/blocking", &status); err != nil {
		t.Fatalf("expected the retry to absorb the dropped connections, got: %v", err)
	}
	if status.Blocking != "enabled" {
		t.Errorf("Blocking = %q, want %q", status.Blocking, "enabled")
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("server saw %d attempts, want 3", got)
	}
}

func TestRetry_SeatsExceeded_IsNotRetried(t *testing.T) {
	var authAttempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			authAttempts.Add(1)
			errorBody(w, http.StatusTooManyRequests, seatsExceededKey,
				"API seats exceeded", "increase webserver.api.max_sessions")
			return
		}
		t.Errorf("request reached %s despite the session pool being full", r.URL.Path)
	}))
	defer srv.Close()

	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), fastRetry(3))

	err := c.Get(context.Background(), "/dns/blocking", nil)
	if err == nil {
		t.Fatal("expected an error")
	}

	// A seat is only freed by a session timing out (30 minutes) or being
	// revoked, so spending the retry budget on it is pure delay.
	var sessErr *SessionLimitError
	if !errors.As(err, &sessErr) {
		t.Fatalf("error is %T, want *SessionLimitError: %v", err, err)
	}
	if got := authAttempts.Load(); got != 1 {
		t.Errorf("login was attempted %d times, want exactly 1 (seat exhaustion must not be retried)", got)
	}

	// The message has to tell the user how to fix it themselves.
	for _, want := range []string{"max_sessions", "pihole_auth_revoke_session"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message does not mention %q; got: %s", want, err)
		}
	}
}

func TestRetry_RateLimit_IsRetried(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK(w, r) {
			return
		}
		// FTL's failed-login limiter clears within seconds, so a 429 that is not
		// seat exhaustion is worth waiting out.
		if attempts.Add(1) == 1 {
			errorBody(w, http.StatusTooManyRequests, "rate_limit", "Too many requests", "")
			return
		}
		writeJSON(w, BlockingStatus{Blocking: "enabled"})
	}))
	defer srv.Close()

	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), fastRetry(3))

	if err := c.Get(context.Background(), "/dns/blocking", nil); err != nil {
		t.Fatalf("expected the rate limit to be retried, got: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("server saw %d attempts, want 2", got)
	}
}

func TestRetry_MutatingRequest_NotReplayedOn5xx(t *testing.T) {
	var deletes atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK(w, r) {
			return
		}
		deletes.Add(1)
		errorBody(w, http.StatusBadGateway, "server_error", "Bad gateway", "")
	}))
	defer srv.Close()

	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), fastRetry(3))

	if err := c.Delete(context.Background(), "/domains/deny/exact/example.com"); err == nil {
		t.Fatal("expected an error")
	}

	// We cannot know whether Pi-hole applied the delete before failing to
	// answer, so replaying it risks a second, different outcome.
	if got := deletes.Load(); got != 1 {
		t.Errorf("DELETE was sent %d times, want exactly 1 (a mutating request must never be replayed)", got)
	}
}

func TestRetry_GET_IsRetriedOn5xx(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK(w, r) {
			return
		}
		if attempts.Add(1) <= 2 {
			errorBody(w, http.StatusServiceUnavailable, "server_error", "FTL is restarting", "")
			return
		}
		writeJSON(w, BlockingStatus{Blocking: "enabled"})
	}))
	defer srv.Close()

	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), fastRetry(3))

	if err := c.Get(context.Background(), "/dns/blocking", nil); err != nil {
		t.Fatalf("expected a GET to ride out a 5xx, got: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("server saw %d attempts, want 3", got)
	}
}

func TestRetry_BudgetIsBounded(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK(w, r) {
			return
		}
		attempts.Add(1)
		errorBody(w, http.StatusServiceUnavailable, "server_error", "down", "")
	}))
	defer srv.Close()

	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), fastRetry(2))

	if err := c.Get(context.Background(), "/dns/blocking", nil); err == nil {
		t.Fatal("expected an error once the budget was spent")
	}
	// One initial attempt plus two retries.
	if got := attempts.Load(); got != 3 {
		t.Errorf("server saw %d attempts, want 3 (1 initial + 2 retries)", got)
	}
}

func TestRetry_Disabled(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK(w, r) {
			return
		}
		attempts.Add(1)
		errorBody(w, http.StatusServiceUnavailable, "server_error", "down", "")
	}))
	defer srv.Close()

	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), WithRetry(0, 0))

	if err := c.Get(context.Background(), "/dns/blocking", nil); err == nil {
		t.Fatal("expected an error")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("server saw %d attempts, want 1 (PIHOLE_MAX_RETRIES=0 disables retrying)", got)
	}
}

func TestRetry_ContextCancellationAbortsBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK(w, r) {
			return
		}
		errorBody(w, http.StatusServiceUnavailable, "server_error", "down", "")
	}))
	defer srv.Close()

	// A long backoff that the cancellation must cut short rather than wait out.
	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), WithRetry(5, 30*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := c.Get(ctx, "/dns/blocking", nil); err == nil {
		t.Fatal("expected an error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s — cancellation did not interrupt the backoff sleep", elapsed)
	}
}

func TestRetry_HonoursRetryAfter(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK(w, r) {
			return
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			errorBody(w, http.StatusTooManyRequests, "rate_limit", "Too many requests", "")
			return
		}
		writeJSON(w, BlockingStatus{Blocking: "enabled"})
	}))
	defer srv.Close()

	// MaxDelay is well below the header's 1s, so the header must be the thing
	// that lengthens the wait — and MaxDelay must still cap it.
	c := New(srv.URL, "pw", WithHTTPClient(srv.Client()), WithRetry(3, 300*time.Millisecond))

	start := time.Now()
	if err := c.Get(context.Background(), "/dns/blocking", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waited %s — Retry-After should have been capped by MaxDelay", elapsed)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"absent", "", 0},
		{"delay seconds", "5", 5 * time.Second},
		{"zero", "0", 0},
		{"negative is ignored", "-3", 0},
		{"garbage is ignored", "soon", 0},
		{"date in the past", "Mon, 02 Jan 2006 15:04:05 GMT", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.value != "" {
				h.Set("Retry-After", tt.value)
			}
			if got := parseRetryAfter(h); got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}

func TestBackoffDelay(t *testing.T) {
	const maxDelay = 8 * time.Second

	// Equal jitter: each wait falls in [d/2, d] where d doubles per attempt and
	// is capped at maxDelay.
	for attempt := range 8 {
		d := min(baseRetryDelay<<attempt, maxDelay)
		if baseRetryDelay<<attempt <= 0 { // shift overflow
			d = maxDelay
		}
		lo, hi := d/2, d

		for range 50 {
			got := backoffDelay(attempt, maxDelay)
			if got < lo || got > hi {
				t.Fatalf("attempt %d: delay %s outside [%s, %s]", attempt, got, lo, hi)
			}
		}
	}
}

func TestRetryable_ContextErrorsAreNotRetried(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		if retryable(err, true) {
			t.Errorf("retryable(%v) = true, want false — the caller is already gone", err)
		}
	}
}
