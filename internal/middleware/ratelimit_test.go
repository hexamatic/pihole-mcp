package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
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

	// Seed a bucket then mark the session, but not the address, stale by hand.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "old")
	rl.Middleware(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	addr := hostOnly(req.RemoteAddr)
	rl.mu.Lock()
	entry, ok := rl.ips[addr]
	if !ok {
		rl.mu.Unlock()
		t.Fatalf("expected an entry for %s", addr)
	}
	if _, ok := entry.sessions["old"]; !ok {
		rl.mu.Unlock()
		t.Fatal("expected session bucket to be created")
	}
	entry.sessions["old"].lastSeen = time.Now().Add(-2 * idleSessionTTL)
	rl.mu.Unlock()

	rl.evictIdle(time.Now())

	rl.mu.Lock()
	_, sessionPresent := rl.ips[addr].sessions["old"]
	rl.mu.Unlock()
	if sessionPresent {
		t.Fatal("expected idle session bucket to be evicted")
	}
}

func TestRateLimiter_EvictsIdleAddresses(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	defer rl.Stop()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	rl.Middleware(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	addr := hostOnly(req.RemoteAddr)
	rl.mu.Lock()
	rl.ips[addr].lastSeen = time.Now().Add(-2 * idleSessionTTL)
	rl.mu.Unlock()

	rl.evictIdle(time.Now())

	rl.mu.Lock()
	_, present := rl.ips[addr]
	rl.mu.Unlock()
	if present {
		t.Fatal("expected idle address entry to be evicted")
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

// TestRateLimiter_RotatingSessionIDsAreStillLimited is the regression test for
// the bypass: Mcp-Session-Id is a client-supplied header, so keying on it alone
// meant a caller that sent a fresh one on every request never met the limiter.
func TestRateLimiter_RotatingSessionIDsAreStillLimited(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	var ok, limited int
	for i := 0; i < 200; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "203.0.113.9:40000"
		req.Header.Set("Mcp-Session-Id", "forged-"+itoa(i))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		switch rec.Code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected status %d", rec.Code)
		}
	}
	if ok == 0 {
		t.Fatal("expected the address ceiling's burst to allow some requests")
	}
	if limited == 0 {
		t.Fatal("200 requests from one address with a fresh session id each time were never throttled")
	}
}

// TestRateLimiter_SessionBucketsBoundedPerAddress covers the memory half of the
// same bypass: rotating session ids must not mint an unbounded number of buckets.
func TestRateLimiter_SessionBucketsBoundedPerAddress(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	for i := 0; i < maxSessionsPerIP*3; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "203.0.113.10:40000"
		req.Header.Set("Mcp-Session-Id", "rotating-"+itoa(i))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if got := len(rl.ips); got != 1 {
		t.Fatalf("expected 1 tracked address, got %d", got)
	}
	if got := len(rl.ips["203.0.113.10"].sessions); got > maxSessionsPerIP {
		t.Fatalf("session buckets for one address = %d, want at most %d", got, maxSessionsPerIP)
	}
}

// TestRateLimiter_EphemeralPortsShareOneCeiling pins the reason the key is the
// address and not RemoteAddr: the port changes per connection, so keying on the
// pair would hand every new connection a fresh bucket.
func TestRateLimiter_EphemeralPortsShareOneCeiling(t *testing.T) {
	rl := NewRateLimiter(60, 2)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	for i := 0; i < 5; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "198.51.100.4:" + itoa(40000+i)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if got := len(rl.ips); got != 1 {
		t.Fatalf("five connections from one address produced %d buckets, want 1", got)
	}
}

func TestRateLimiter_DistinctAddressesIndependent(t *testing.T) {
	rl := NewRateLimiter(60, 2)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	for i := 0; i < 40; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "198.51.100.5:40000"
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "198.51.100.6:40000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a second address should be unaffected by the first exhausting its ceiling, got %d", rec.Code)
	}
}

func TestClientIPResolver(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.5/32"),
	}

	tests := []struct {
		name       string
		trusted    []netip.Prefix
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{
			name:       "no trusted proxies ignores the header entirely",
			remoteAddr: "203.0.113.1:9000",
			forwarded:  []string{"198.51.100.99"},
			want:       "203.0.113.1",
		},
		{
			name:       "untrusted peer cannot forge a client address",
			trusted:    trusted,
			remoteAddr: "203.0.113.1:9000",
			forwarded:  []string{"198.51.100.99"},
			want:       "203.0.113.1",
		},
		{
			name:       "trusted proxy supplies the client address",
			trusted:    trusted,
			remoteAddr: "10.1.2.3:9000",
			forwarded:  []string{"203.0.113.7"},
			want:       "203.0.113.7",
		},
		{
			name:       "rightmost untrusted entry wins over a spoofed prefix",
			trusted:    trusted,
			remoteAddr: "10.1.2.3:9000",
			forwarded:  []string{"1.1.1.1, 203.0.113.7, 10.9.9.9"},
			want:       "203.0.113.7",
		},
		{
			name:       "repeated headers are one chain",
			trusted:    trusted,
			remoteAddr: "10.1.2.3:9000",
			forwarded:  []string{"1.1.1.1", "203.0.113.8, 10.9.9.9"},
			want:       "203.0.113.8",
		},
		{
			name:       "an unparseable entry stops the walk at the proxy",
			trusted:    trusted,
			remoteAddr: "10.1.2.3:9000",
			forwarded:  []string{"203.0.113.7, not-an-ip"},
			want:       "10.1.2.3",
		},
		{
			name:       "all entries trusted falls back to the peer",
			trusted:    trusted,
			remoteAddr: "10.1.2.3:9000",
			forwarded:  []string{"10.4.4.4, 192.168.1.5"},
			want:       "10.1.2.3",
		},
		{
			name:       "IPv4-mapped IPv6 normalises to the v4 form",
			trusted:    trusted,
			remoteAddr: "10.1.2.3:9000",
			forwarded:  []string{"::ffff:203.0.113.7"},
			want:       "203.0.113.7",
		},
		{
			name:       "bracketed IPv6 peer keeps its address",
			remoteAddr: "[2001:db8::1]:9000",
			want:       "2001:db8::1",
		},
		{
			name:       "a port on a forwarded entry is stripped",
			trusted:    trusted,
			remoteAddr: "10.1.2.3:9000",
			forwarded:  []string{"203.0.113.7:51234"},
			want:       "203.0.113.7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.forwarded {
				req.Header.Add("X-Forwarded-For", v)
			}
			res := ClientIPResolver{trusted: tt.trusted}
			if got := res.ClientIP(req); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRateLimiter_TrustedProxyKeepsClientsApart is the reverse-proxy regression:
// two clients arriving through the same proxy must not share one bucket.
func TestRateLimiter_TrustedProxyKeepsClientsApart(t *testing.T) {
	rl := NewRateLimiter(60, 2, WithTrustedProxies([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}))
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	send := func(client string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "10.0.0.1:9000"
		req.Header.Set("X-Forwarded-For", client)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 40; i++ {
		send("203.0.113.20")
	}
	if code := send("203.0.113.21"); code != http.StatusOK {
		t.Fatalf("a second client behind the same proxy was throttled by the first, got %d", code)
	}
}

func TestRateKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"192.168.1.5", "192.168.1.5"},
		{"::ffff:192.168.1.5", "192.168.1.5"},
		{"2001:db8:1:2::1", "2001:db8:1:2::/64"},
		{"2001:db8:1:2::9999", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"not-an-address", "not-an-address"},
	}
	for _, tt := range tests {
		if got := rateKey(tt.in); got != tt.want {
			t.Errorf("rateKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestRateLimiter_IPv6AddressesInOnePrefixShareACeiling: a host is routinely
// handed a whole /64, so keying on the full address would give one machine as
// many ceilings as it cared to invent.
func TestRateLimiter_IPv6AddressesInOnePrefixShareACeiling(t *testing.T) {
	rl := NewRateLimiter(60, 2)
	defer rl.Stop()
	h := rl.Middleware(okHandler())

	for i := 0; i < 50; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "[2001:db8:1:2::" + itoa(i+1) + "]:9000"
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if got := len(rl.ips); got != 1 {
		t.Fatalf("50 addresses in one /64 produced %d ceilings, want 1", got)
	}
	if _, ok := rl.ips["2001:db8:1:2::/64"]; !ok {
		t.Fatalf("expected the /64 as the key, got %v", rl.ips)
	}
}

func TestRateLimiter_TrackedAddressesBounded(t *testing.T) {
	rl := NewRateLimiter(60, 2)
	defer rl.Stop()

	for i := 0; i < maxTrackedAddresses+50; i++ {
		rl.limitersFor("client-"+itoa(i), "")
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if got := len(rl.ips); got > maxTrackedAddresses {
		t.Fatalf("tracked addresses = %d, want at most %d", got, maxTrackedAddresses)
	}
	if _, ok := rl.ips["client-0"]; ok {
		t.Error("expected the least recently seen address to be evicted first")
	}
	if _, ok := rl.ips["client-"+itoa(maxTrackedAddresses+49)]; !ok {
		t.Error("expected the most recent address to be kept")
	}
}

func TestLogSafeKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"192.168.1.5", "192.168.1.5"},
		{"2001:db8:1:2::/64", "2001:db8:1:2::/64"},
		{"192.168.1.5\nfake log line", "an unrecognised address"},
		{"", "an unrecognised address"},
	}
	for _, tt := range tests {
		if got := logSafeKey(tt.in); got != tt.want {
			t.Errorf("logSafeKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
