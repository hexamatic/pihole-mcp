package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimiter_UnderLimitAllowed(t *testing.T) {
	rl := NewRateLimiter(120, 30)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	for i := 0; i < 10; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.Header.Set("Mcp-Session-Id", "session-a")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i, rec.Code)
		}
	}
}

func TestRateLimiter_OverLimitReturns429(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	var ok, limited int
	for i := 0; i < 50; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.Header.Set("Mcp-Session-Id", "session-burst")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		switch rec.Code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			limited++
			if rec.Header().Get("Retry-After") != "1" {
				t.Errorf("missing or wrong Retry-After: %q", rec.Header().Get("Retry-After"))
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("missing Content-Type: %q", rec.Header().Get("Content-Type"))
			}
		default:
			t.Fatalf("unexpected status %d", rec.Code)
		}
	}
	if ok == 0 {
		t.Fatal("expected at least some 200s before throttling kicked in")
	}
	if limited == 0 {
		t.Fatal("expected at least some 429s after burst exhausted")
	}
}

func TestRateLimiter_DistinctSessionsIndependent(t *testing.T) {
	rl := NewRateLimiter(60, 2)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	ctx := t.Context()
	exhaust := func(sid string) {
		for i := 0; i < 5; i++ {
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp", nil)
			req.Header.Set("Mcp-Session-Id", sid)
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	}
	exhaust("session-1")

	// session-2 should still be fresh
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "session-2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session-2 should be unaffected by session-1 exhaustion, got %d", rec.Code)
	}
}

func TestRateLimiter_ZeroDisables(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	for i := 0; i < 1000; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.Header.Set("Mcp-Session-Id", "any")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: zero rate-limit should pass through, got %d", i, rec.Code)
		}
	}
}

func TestRateLimiter_FallsBackToRemoteAddr(t *testing.T) {
	rl := NewRateLimiter(60, 2)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	// No session ID header — must use RemoteAddr
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request with RemoteAddr key should pass, got %d", rec.Code)
	}
}

func TestRateLimiter_EvictsIdleSessions(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	defer rl.Stop()

	// Seed a bucket then mark it stale by hand
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "old")
	rl.Middleware(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	rl.mu.Lock()
	if _, ok := rl.buckets["sid:old"]; !ok {
		rl.mu.Unlock()
		t.Fatal("expected bucket to be created")
	}
	rl.buckets["sid:old"].lastSeen = time.Now().Add(-2 * idleSessionTTL)
	rl.mu.Unlock()

	rl.evictIdle(time.Now())

	rl.mu.Lock()
	_, stillPresent := rl.buckets["sid:old"]
	rl.mu.Unlock()
	if stillPresent {
		t.Fatal("expected idle bucket to be evicted")
	}
}

func TestRateLimiter_ConcurrentSessionsRaceClean(t *testing.T) {
	rl := NewRateLimiter(600, 100)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	var wg sync.WaitGroup
	ctx := t.Context()
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp", nil)
				req.Header.Set("Mcp-Session-Id", "concurrent-"+itoa(id))
				h.ServeHTTP(httptest.NewRecorder(), req)
			}
		}(i)
	}
	wg.Wait()
}

func TestComputeBurst(t *testing.T) {
	tests := []struct {
		perMinute int
		want      int
	}{
		{0, 30},     // floor
		{60, 30},    // floor
		{120, 30},   // 30
		{200, 50},   // 200/4
		{1200, 300}, // big
	}
	for _, tt := range tests {
		if got := ComputeBurst(tt.perMinute); got != tt.want {
			t.Errorf("ComputeBurst(%d) = %d, want %d", tt.perMinute, got, tt.want)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
