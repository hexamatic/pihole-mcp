package pihole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// login performs POST /api/auth to obtain a session ID.
func (c *Client) login(ctx context.Context) error {
	body, err := json.Marshal(authRequest{Password: c.password}) //nolint:gosec // password is not a hardcoded credential
	if err != nil {
		return fmt.Errorf("marshalling auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/auth", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &loginError{fmt.Errorf("sending auth request: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &loginError{fmt.Errorf("reading auth response: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		apiErr := c.parseError(resp.StatusCode, resp.Header, "/api/auth", respBody)

		// A 401 is a genuinely bad password. Anything else — a full session pool,
		// the failed-login limiter, a wedged server — is Pi-hole declining to
		// answer, not a credentials problem, and must not be reported as one.
		var authErr *AuthError
		if !errors.As(apiErr, &authErr) {
			return &loginError{apiErr}
		}
		authErr.Message = "authentication failed: " + authErr.Message
		return authErr
	}

	var authResp authResponse
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		return fmt.Errorf("unmarshalling auth response: %w", err)
	}

	if !authResp.Session.Valid {
		return &AuthError{&APIError{
			StatusCode: http.StatusUnauthorized,
			Key:        "unauthorized",
			Message:    "authentication failed: " + authResp.Session.Message,
			Endpoint:   "/api/auth",
		}}
	}

	// No-password Pi-hole instances return valid=true with no SID.
	// In this case we store an empty SID — the client will still work
	// because Pi-hole won't require auth on subsequent requests.
	c.sid = authResp.Session.SID
	return nil
}

// ensureAuth returns the current SID, performing a login if necessary.
// Caller must NOT hold c.mu.
func (c *Client) ensureAuth(ctx context.Context) (string, error) {
	c.mu.RLock()
	sid := c.sid
	c.mu.RUnlock()

	if sid != "" {
		return sid, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock.
	if c.sid != "" {
		return c.sid, nil
	}

	if err := c.login(ctx); err != nil {
		return "", err
	}
	return c.sid, nil
}

// handleAuthRetry re-authenticates after a 401 response.
// Uses compare-and-swap to avoid thundering herd: if another goroutine
// already refreshed, we use the new SID without re-authenticating.
func (c *Client) handleAuthRetry(ctx context.Context, oldSID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Another goroutine already refreshed.
	if c.sid != oldSID {
		return c.sid, nil
	}

	// Clear stale SID and re-authenticate.
	c.sid = ""
	if err := c.login(ctx); err != nil {
		return "", err
	}
	return c.sid, nil
}
