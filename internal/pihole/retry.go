package pihole

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"syscall"
	"time"
)

const (
	// DefaultMaxRetries is the number of retries attempted after the initial
	// request. Three covers the transient connection drops FTL's embedded web
	// server produces under load without making a genuinely broken Pi-hole feel
	// slow to fail.
	DefaultMaxRetries = 3

	// DefaultMaxRetryDelay caps a single backoff wait.
	DefaultMaxRetryDelay = 8 * time.Second

	// baseRetryDelay is the first backoff interval, doubled per attempt.
	baseRetryDelay = 200 * time.Millisecond
)

// RetryPolicy governs how failed Pi-hole API requests are re-attempted.
// A zero MaxRetries disables retrying entirely.
type RetryPolicy struct {
	MaxRetries int
	MaxDelay   time.Duration
}

// WithRetry sets the retry policy. maxRetries of 0 disables retrying.
func WithRetry(maxRetries int, maxDelay time.Duration) Option {
	return func(c *Client) {
		if maxRetries < 0 {
			maxRetries = 0
		}
		if maxDelay <= 0 {
			maxDelay = DefaultMaxRetryDelay
		}
		c.retry = RetryPolicy{MaxRetries: maxRetries, MaxDelay: maxDelay}
	}
}

// withRetry runs do, re-attempting it while the failure looks transient.
//
// idempotent must be true only when replaying the request cannot change the
// outcome. When a request fails without a response we cannot know whether the
// server processed it, so a mutating request is never replayed — a duplicated
// "delete domain" or "add client" is a worse outcome than surfacing the error.
func (c *Client) withRetry(ctx context.Context, idempotent bool, do func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		err = do()
		if err == nil {
			return nil
		}
		if attempt >= c.retry.MaxRetries || !retryable(err, idempotent) {
			return err
		}
		if sleepErr := sleepBackoff(ctx, attempt, c.retry.MaxDelay, err); sleepErr != nil {
			// The caller gave up while we were backing off. Report what actually
			// went wrong with Pi-hole rather than the cancellation it caused.
			return err
		}
	}
}

// retryable reports whether err is worth another attempt.
func retryable(err error, idempotent bool) bool {
	// The caller is gone; nothing to retry for.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Session-pool exhaustion is a 429, but a seat is only freed by a session
	// timing out (30 minutes by default) or being revoked. Retrying spends the
	// whole budget and still fails, so surface it immediately with advice.
	var sessErr *SessionLimitError
	if errors.As(err, &sessErr) {
		return false
	}

	// Any other 429 is FTL's failed-login limiter, which decays within seconds.
	// Safe for mutating requests too: a rate-limited request was rejected before
	// it was processed, so replaying it cannot double-apply anything.
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		return true
	}

	// The session could not be established, so the caller's request was never
	// sent. Replaying it is safe whatever method it used.
	var loginErr *loginError
	if errors.As(err, &loginErr) {
		return serverUnavailable(err) || transientNetErr(err)
	}

	// Past this point the request may or may not have reached Pi-hole, so only
	// replay when doing so is harmless.
	if !idempotent {
		return false
	}

	return serverUnavailable(err) || transientNetErr(err)
}

// serverUnavailable reports a 5xx — Pi-hole is up but could not answer
// (FTL restarting, gravity rebuild in progress).
func serverUnavailable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}
	return false
}

// transientNetErr reports whether err is a connection-level failure that tends
// to succeed on a second attempt. FTL's embedded web server drops connections
// under concurrent load, which surfaces as a bare EOF and previously failed the
// tool call outright.
func transientNetErr(err error) bool {
	switch {
	case errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.EPIPE):
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// sleepBackoff waits before the next attempt, or returns early if ctx ends.
func sleepBackoff(ctx context.Context, attempt int, maxDelay time.Duration, err error) error {
	d := backoffDelay(attempt, maxDelay)

	// A server that tells us when to come back knows better than our guess.
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		d = min(apiErr.RetryAfter, maxDelay)
	}

	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// backoffDelay returns an exponentially increasing wait with equal jitter:
// half the interval is fixed, half is random. The jitter matters because
// instance=all fans out concurrently — without it, every instance that hits the
// same hiccup would back off in lockstep and retry as a second thundering herd.
func backoffDelay(attempt int, maxDelay time.Duration) time.Duration {
	d := baseRetryDelay << attempt
	if d <= 0 || d > maxDelay { // <= 0 guards the shift overflowing
		d = maxDelay
	}
	half := d / 2
	// Spreading retries in time is not a security decision — an attacker who can
	// predict our jitter learns nothing — so the cheap PRNG is the right one.
	return half + time.Duration(rand.Int64N(int64(half)+1)) //nolint:gosec // G404: jitter needs spread, not unpredictability
}

// idempotentMethod reports whether replaying a request of this method is safe.
// Only GET qualifies. DELETE is idempotent in HTTP's sense, but Pi-hole answers
// a repeated delete with 404 — so a replayed DELETE whose first response was
// lost would report "not found" instead of success, which is worse than the
// transient error it was meant to paper over.
func idempotentMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}
