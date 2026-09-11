package middleware

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/time/rate"
)

const (
	// idleSessionTTL is how long an idle bucket lives before sweep removes it.
	idleSessionTTL = 3 * time.Minute
	// sweepInterval is how often the background cleanup runs.
	sweepInterval = 5 * time.Minute
	// minBurst is the floor for the burst capacity computation.
	minBurst = 30

	// ipCeilingFactor is how much more traffic one client address may send than
	// one session. The ceiling is what actually bounds a caller, because the
	// session sub-key is a client-supplied header: without it, a caller sending
	// a fresh Mcp-Session-Id on every request gets a fresh bucket every time and
	// is not limited at all. Four sessions' worth leaves room for a client that
	// legitimately multiplexes (an MCP client with several connections open, or
	// a couple of people on one machine) while keeping a forged-session flood
	// bounded at a known multiple.
	ipCeilingFactor = 4

	// maxTrackedAddresses caps the whole table. Past it, the least recently seen
	// address is dropped to make room, so a flood of source addresses costs a
	// bounded amount of memory and evicts nothing but idle strangers. Roughly
	// 2.5 MB at this size.
	maxTrackedAddresses = 8192

	// failedAuthPerMinute and failedAuthBurst are the budget for requests that
	// were refused before the main limiter ran. It is deliberately separate from
	// PIHOLE_RATE_LIMIT and much tighter: a rejected request is never legitimate
	// traffic, so the curve can be steep, and keeping it separate means a flood
	// of wrong tokens cannot starve the budget of a client at the same address
	// that does hold the token. Twenty attempts straight away, then one a second.
	failedAuthPerMinute = 15
	failedAuthBurst     = 5

	// authWarnInterval is the shortest gap between startup-log complaints about
	// one address failing authentication. Without it the log line an operator
	// needs to see would itself be the flood.
	authWarnInterval = time.Minute

	// maxSessionsPerIP caps how many session buckets one address may create.
	// Past the cap, further session ids from that address are governed by the
	// address ceiling alone rather than each minting a map entry, so a caller
	// rotating session ids cannot grow the map without bound. 64 is far above
	// any real client and far below a memory problem.
	maxSessionsPerIP = 64
)

// RateLimiter is a two-level token-bucket rate limiter for the HTTP and SSE
// transports. Every request is charged first to a per-client-address ceiling
// and then, if the request names a session, to that session's own bucket.
//
// The address is the trustworthy half of the key and the session is not, which
// is why the ceiling comes first. Mcp-Session-Id is a header the caller writes;
// keying solely on it means a caller who varies it is never limited. Keying
// solely on the address is the opposite mistake: every client behind a reverse
// proxy shares one bucket, and RemoteAddr carries the ephemeral port, so it
// identifies a connection rather than a client. Hence: address for the ceiling
// (port stripped, X-Forwarded-For honoured only from a trusted proxy), session
// for fair sharing underneath it.
type RateLimiter struct {
	perMinute    int
	burst        int
	ipLimit      rate.Limit
	sessionLimit rate.Limit
	resolver     ClientIPResolver

	mu       sync.Mutex
	ips      map[string]*ipEntry
	stopOnce sync.Once
	stopCh   chan struct{}
}

// ipEntry is one client address: its ceiling bucket and the session buckets
// nested under it.
type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
	lastWarn time.Time
	sessions map[string]*bucket
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Option configures a RateLimiter.
type Option func(*RateLimiter)

// WithTrustedProxies makes the limiter read the client address from
// X-Forwarded-For when the immediate peer falls inside one of these prefixes.
// With no trusted proxies (the default) the header is ignored entirely.
func WithTrustedProxies(prefixes []netip.Prefix) Option {
	return func(rl *RateLimiter) { rl.resolver = ClientIPResolver{trusted: prefixes} }
}

// NewRateLimiter constructs a rate limiter that allows perMinute requests per
// session per minute with the given burst capacity, under a per-address
// ceiling of ipCeilingFactor times that rate. perMinute=0 returns a limiter
// that lets every request through (a no-op).
//
// Burst defaults are computed by ComputeBurst when callers don't have a
// specific value in mind: max(perMinute/4, 30).
func NewRateLimiter(perMinute, burst int, opts ...Option) *RateLimiter {
	sessionLimit := rate.Limit(float64(perMinute) / 60.0)
	rl := &RateLimiter{
		perMinute:    perMinute,
		burst:        burst,
		sessionLimit: sessionLimit,
		ipLimit:      sessionLimit * ipCeilingFactor,
		ips:          make(map[string]*ipEntry),
		stopCh:       make(chan struct{}),
	}
	for _, opt := range opts {
		opt(rl)
	}
	if perMinute > 0 {
		go rl.sweep()
	}
	return rl
}

// NewFailureLimiter returns the limiter that charges requests refused before the
// main limiter runs. Wire it into BearerAuth with WithFailurePenalty.
//
// It is always on, including when PIHOLE_RATE_LIMIT is 0: disabling rate
// limiting is a statement about legitimate traffic, usually because a proxy in
// front is already doing it, and it should not also switch off the only thing
// standing between a shared token and an unlimited guessing rate.
func NewFailureLimiter(opts ...Option) *RateLimiter {
	return NewRateLimiter(failedAuthPerMinute, failedAuthBurst, opts...)
}

// ComputeBurst returns the default burst capacity for a given per-minute rate.
func ComputeBurst(perMinute int) int {
	b := perMinute / 4
	if b < minBurst {
		b = minBurst
	}
	return b
}

// Middleware wraps next with rate-limit enforcement. When perMinute is 0 the
// wrapper returns next unmodified.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	if rl.perMinute == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ipLimiter, sessionLimiter := rl.limitersFor(rateKey(rl.resolver.ClientIP(r)), sessionID(r))
		// The ceiling is charged first and always, so a caller that rotates
		// session ids still pays for every request it makes.
		if !ipLimiter.Allow() {
			rejectRateLimited(w)
			return
		}
		if sessionLimiter != nil && !sessionLimiter.Allow() {
			rejectRateLimited(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Stop terminates the background sweep goroutine. Safe to call multiple times.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stopCh) })
}

// limitersFor returns the ceiling limiter for key and, when the request names a
// session that is within the per-address cap, that session's limiter. A nil
// session limiter means the ceiling alone governs the request.
func (rl *RateLimiter) limitersFor(key, sid string) (*rate.Limiter, *rate.Limiter) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry := rl.entryForLocked(key, now)

	if sid == "" {
		return entry.limiter, nil
	}
	if b, ok := entry.sessions[sid]; ok {
		b.lastSeen = now
		return entry.limiter, b.limiter
	}
	if len(entry.sessions) >= maxSessionsPerIP {
		return entry.limiter, nil
	}
	b := &bucket{limiter: rate.NewLimiter(rl.sessionLimit, rl.burst), lastSeen: now}
	entry.sessions[sid] = b
	return entry.limiter, b.limiter
}

// Penalise charges one request to the ceiling for the request's client address
// without consulting any session bucket, and reports whether the ceiling still
// allows it. It returns true when rate limiting is disabled.
//
// This is how a request rejected before the limiter runs still costs its sender
// something. Authentication answers 401 ahead of the limiter so that an
// unauthenticated caller cannot spend an authenticated client's budget, but
// without this that ordering would also mean a wrong token costs nothing at all
// and a token could be guessed at line rate, silently.
func (rl *RateLimiter) Penalise(r *http.Request) bool {
	if rl.perMinute == 0 {
		return true
	}
	key := rateKey(rl.resolver.ClientIP(r))

	rl.mu.Lock()
	now := time.Now()
	entry := rl.entryForLocked(key, now)
	allowed := entry.limiter.Allow()
	warn := !allowed && now.Sub(entry.lastWarn) > authWarnInterval
	if warn {
		entry.lastWarn = now
	}
	rl.mu.Unlock()

	if warn {
		// %q, and only ever a key that parsed as an address or a prefix: with a
		// trusted proxy configured the address is derived from X-Forwarded-For,
		// which the client writes, and a newline in it would otherwise let the
		// client forge log lines.
		//nolint:gosec // G706: logSafeKey admits only a parsed address or prefix, so nothing the client wrote reaches the log
		log.Printf("ERROR: throttling %q, which is repeatedly sending requests this server refuses. "+
			"If authentication is enabled, something is guessing the token.", logSafeKey(key))
	}
	return allowed
}

// logSafeKey returns key when it is a bare address or prefix, and a placeholder
// otherwise, so nothing a client chose reaches the log verbatim.
func logSafeKey(key string) string {
	if _, err := netip.ParseAddr(key); err == nil {
		return key
	}
	if _, err := netip.ParsePrefix(key); err == nil {
		return key
	}
	return "an unrecognised address"
}

// entryForLocked returns the entry for key, creating it if needed. Callers must
// hold rl.mu.
func (rl *RateLimiter) entryForLocked(key string, now time.Time) *ipEntry {
	if entry, ok := rl.ips[key]; ok {
		entry.lastSeen = now
		return entry
	}
	if len(rl.ips) >= maxTrackedAddresses {
		rl.evictOldestLocked()
	}
	entry := &ipEntry{
		limiter:  rate.NewLimiter(rl.ipLimit, rl.burst*ipCeilingFactor),
		lastSeen: now,
		sessions: make(map[string]*bucket),
	}
	rl.ips[key] = entry
	return entry
}

// evictOldestLocked drops the least recently seen address so the table stays
// bounded. Evicting rather than refusing means a flood of source addresses costs
// the clients it displaces nothing worse than a fresh bucket. Callers must hold
// rl.mu.
func (rl *RateLimiter) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, entry := range rl.ips {
		if oldestKey == "" || entry.lastSeen.Before(oldest) {
			oldestKey, oldest = key, entry.lastSeen
		}
	}
	delete(rl.ips, oldestKey)
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

// evictIdle drops addresses that have gone quiet, and idle session buckets
// under addresses that are still active. The second half matters on its own: a
// long-lived client that opens a new session periodically would otherwise
// accumulate buckets for as long as it kept the address busy.
func (rl *RateLimiter) evictIdle(now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for ip, entry := range rl.ips {
		if now.Sub(entry.lastSeen) > idleSessionTTL {
			delete(rl.ips, ip)
			continue
		}
		for sid, b := range entry.sessions {
			if now.Sub(b.lastSeen) > idleSessionTTL {
				delete(entry.sessions, sid)
			}
		}
	}
}

// sessionID returns the MCP session identifier the request claims, or "" when
// it names none.
//
// Treat this as a hint, never as an identity. It is a client-supplied header
// with no server-side proof, which is why it is only ever a sub-key beneath the
// address ceiling in limitersFor.
//
// It is also on the way out: MCP revision 2026-07-28 removes protocol-level
// sessions and the Mcp-Session-Id header from the Streamable HTTP transport
// entirely, and instructs servers to ignore the header if an older client still
// sends one (SEP-2567). After that upgrade this function returns "" for every
// request and the address ceiling becomes the whole limiter. That is precisely
// why the ceiling exists: before this two-level keying, the same change would
// have moved every request onto a single RemoteAddr bucket that includes the
// ephemeral port, which is a fresh bucket per connection, so the limiter would
// have stopped limiting silently rather than degrading. Re-keying was therefore
// a prerequisite of the protocol upgrade, not a follow-up to it.
func sessionID(r *http.Request) string {
	return r.Header.Get(server.HeaderKeySessionID)
}

// ClientIPResolver works out which client a request came from.
//
// The zero value trusts no proxy and always uses the immediate peer address,
// which is the safe default: X-Forwarded-For is client-supplied, so believing
// it unconditionally would let any caller choose its own rate-limit bucket.
type ClientIPResolver struct {
	trusted []netip.Prefix
}

// ClientIP returns the address the request is charged to, with the port
// stripped. RemoteAddr's port is per-connection, so leaving it in would give a
// client a fresh bucket for every connection it opened.
//
// When the immediate peer is a trusted proxy, the rightmost X-Forwarded-For
// entry that is not itself a trusted proxy is used instead. Rightmost rather
// than leftmost: the left of the list is whatever the original caller sent and
// can say anything, while each proxy appends the peer it actually saw.
func (res ClientIPResolver) ClientIP(r *http.Request) string {
	peer := hostOnly(r.RemoteAddr)
	peerAddr, err := netip.ParseAddr(peer)
	if err == nil {
		// An IPv4-mapped IPv6 peer and the same host in dotted form must not
		// reach two different buckets.
		peer = peerAddr.Unmap().String()
	}
	if len(res.trusted) == 0 || err != nil || !res.isTrusted(peerAddr) {
		return peer
	}
	forwarded := forwardedChain(r)
	for i := len(forwarded) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(hostOnly(forwarded[i]))
		if err != nil {
			// An entry we cannot parse means the chain stopped being
			// trustworthy here. Anything further left is attacker-writable, so
			// charge the proxy rather than guess.
			return peer
		}
		if !res.isTrusted(addr.Unmap()) {
			return addr.Unmap().String()
		}
	}
	return peer
}

// rateKey collapses a client address into the identity buckets are kept under.
//
// IPv6 is keyed on the /64 rather than the address. A single host is routinely
// given a whole /64 and can pick a fresh address out of it per request, so
// keying on the address would hand one machine 2^64 independent ceilings.
func rateKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

func (res ClientIPResolver) isTrusted(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range res.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// forwardedChain flattens every X-Forwarded-For header on the request into one
// ordered list. The header may legitimately appear more than once, and Go keeps
// each occurrence separately.
func forwardedChain(r *http.Request) []string {
	var out []string
	for _, header := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(header, ",") {
			if entry := strings.TrimSpace(part); entry != "" {
				out = append(out, entry)
			}
		}
	}
	return out
}

// hostOnly strips a trailing port from an address, tolerating bare addresses
// and bracketed IPv6 literals.
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return strings.Trim(addr, "[]")
}

func rejectRateLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":             "rate_limited",
		"retryAfterSeconds": 1,
	})
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
