package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/time/rate"
)

const (
	// idleSessionTTL is how long an idle session bucket lives before sweep removes it.
	idleSessionTTL = 3 * time.Minute
	// sweepInterval is how often the background cleanup runs.
	sweepInterval = 5 * time.Minute
	// minBurst is the floor for the burst capacity computation.
	minBurst = 30
)

// RateLimiter is a per-session token-bucket rate limiter. Each session
// (identified by the Mcp-Session-Id header, or RemoteAddr as fallback) gets
// its own bucket. Idle buckets are reaped by a background goroutine.
type RateLimiter struct {
	perMinute int
	burst     int

	mu       sync.Mutex
	buckets  map[string]*bucket
	stopOnce sync.Once
	stopCh   chan struct{}
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter constructs a rate limiter that allows perMinute requests per
// session per minute with the given burst capacity. perMinute=0 returns a
// limiter that lets every request through (a no-op).
//
// Burst defaults are computed by ComputeBurst when callers don't have a
// specific value in mind: max(perMinute/4, 30).
func NewRateLimiter(perMinute, burst int) *RateLimiter {
	rl := &RateLimiter{
		perMinute: perMinute,
		burst:     burst,
		buckets:   make(map[string]*bucket),
		stopCh:    make(chan struct{}),
	}
	if perMinute > 0 {
		go rl.sweep()
	}
	return rl
}

// ComputeBurst returns the default burst capacity for a given per-minute rate.
func ComputeBurst(perMinute int) int {
	b := perMinute / 4
	if b < minBurst {
		b = minBurst
	}
	return b
}

// Middleware wraps next with per-session rate-limit enforcement. When
// perMinute is 0 the wrapper returns next unmodified.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	if rl.perMinute == 0 {
		return next
	}
	limit := rate.Limit(float64(rl.perMinute) / 60.0)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := sessionKey(r)
		b := rl.bucketFor(key, limit)
		if !b.limiter.Allow() {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "rate_limited",
				"retryAfterSeconds": 1,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Stop terminates the background sweep goroutine. Safe to call multiple times.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stopCh) })
}

func (rl *RateLimiter) bucketFor(key string, limit rate.Limit) *bucket {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if b, ok := rl.buckets[key]; ok {
		b.lastSeen = time.Now()
		return b
	}
	b := &bucket{
		limiter:  rate.NewLimiter(limit, rl.burst),
		lastSeen: time.Now(),
	}
	rl.buckets[key] = b
	return b
}

func (rl *RateLimiter) sweep() {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.evictIdle(time.Now())
		}
	}
}

func (rl *RateLimiter) evictIdle(now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for k, b := range rl.buckets {
		if now.Sub(b.lastSeen) > idleSessionTTL {
			delete(rl.buckets, k)
		}
	}
}

// sessionKey returns the identifier used to bucket the request. Prefers
// Mcp-Session-Id (per the MCP spec) and falls back to RemoteAddr for
// pre-initialise requests or non-conforming clients.
func sessionKey(r *http.Request) string {
	if sid := r.Header.Get(server.HeaderKeySessionID); sid != "" {
		return "sid:" + sid
	}
	return "addr:" + r.RemoteAddr
}

// BindShutdown stops the sweep goroutine when ctx is cancelled.
// Wire to the parent http.Server's shutdown context so the limiter does not
// leak its goroutine across server restarts.
func (rl *RateLimiter) BindShutdown(ctx context.Context) {
	go func() {
		<-ctx.Done()
		rl.Stop()
	}()
}
