package pihole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Client is an HTTP client for the Pi-hole v6 REST API.
// It handles session-based authentication transparently.
type Client struct {
	name       string
	baseURL    string
	password   string
	httpClient *http.Client
	retry      RetryPolicy
	mu         sync.RWMutex
	sid        string
}

// Option configures the Pi-hole client.
type Option func(*Client)

// WithName sets a human-readable instance name used for log and error
// attribution when multiple Pi-holes are configured.
func WithName(name string) Option {
	return func(c *Client) {
		c.name = name
	}
}

// Name returns the configured instance name (empty for single-instance setups
// created without WithName).
func (c *Client) Name() string {
	return c.name
}

// WithTimeout sets the HTTP request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithHTTPClient sets a custom HTTP client (useful for testing).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// New creates a new Pi-hole API client.
func New(baseURL, password string, opts ...Option) *Client {
	c := &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		retry: RetryPolicy{
			MaxRetries: DefaultMaxRetries,
			MaxDelay:   DefaultMaxRetryDelay,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Do executes an authenticated API request.
//
// Transient failures are retried with backoff (see retry.go); a 401 refreshes
// the session and replays the request once, regardless of method — a rejected
// request was never processed, so replaying it is always safe.
func (c *Client) Do(ctx context.Context, method, path string, body, result any) error {
	return c.withRetry(ctx, idempotentMethod(method), func() error {
		sid, err := c.ensureAuth(ctx)
		if err != nil {
			return err
		}

		err = c.doRequest(ctx, method, path, sid, body, result)
		if err == nil {
			return nil
		}

		// On 401, re-authenticate and retry once.
		var authErr *AuthError
		if !isAuthError(err, &authErr) {
			return err
		}

		newSID, retryErr := c.handleAuthRetry(ctx, sid)
		if retryErr != nil {
			return retryErr
		}

		return c.doRequest(ctx, method, path, newSID, body, result)
	})
}

// Get performs an authenticated GET request.
func (c *Client) Get(ctx context.Context, path string, result any) error {
	return c.Do(ctx, http.MethodGet, path, nil, result)
}

// Post performs an authenticated POST request.
func (c *Client) Post(ctx context.Context, path string, body, result any) error {
	return c.Do(ctx, http.MethodPost, path, body, result)
}

// Put performs an authenticated PUT request.
func (c *Client) Put(ctx context.Context, path string, body, result any) error {
	return c.Do(ctx, http.MethodPut, path, body, result)
}

// Delete performs an authenticated DELETE request.
// Returns nil on 204 No Content.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.Do(ctx, http.MethodDelete, path, nil, nil)
}

// PostMultipart uploads a file via multipart form-data with optional JSON import options.
// Used for teleporter import.
//
// The encoded body is built once and replayed from memory on retry. This is an
// upload, so it is not treated as idempotent: only a rate-limited rejection —
// which Pi-hole makes before reading the body — is ever re-sent.
func (c *Client) PostMultipart(ctx context.Context, path, filePath string, importOptions map[string]any, result any) error {
	body, contentType, err := buildMultipartBody(filePath, importOptions)
	if err != nil {
		return err
	}

	return c.withRetry(ctx, false, func() error {
		sid, err := c.ensureAuth(ctx)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api"+path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("creating multipart request: %w", err)
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-FTL-SID", sid)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("sending multipart request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}

		if resp.StatusCode >= 400 {
			return c.parseError(resp.StatusCode, resp.Header, path, respBody)
		}

		if result != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("unmarshalling response: %w", err)
			}
		}

		return nil
	})
}

// buildMultipartBody encodes filePath (and any import options) as a multipart
// form, returning the body and its Content-Type.
func buildMultipartBody(filePath string, importOptions map[string]any) ([]byte, string, error) {
	file, err := os.Open(filePath) //nolint:gosec // file path is user-provided via MCP tool parameter
	if err != nil {
		return nil, "", fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, "", fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", fmt.Errorf("copying file data: %w", err)
	}

	if importOptions != nil {
		optJSON, err := json.Marshal(importOptions)
		if err != nil {
			return nil, "", fmt.Errorf("marshalling import options: %w", err)
		}
		if err := writer.WriteField("import", string(optJSON)); err != nil {
			return nil, "", fmt.Errorf("writing import field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

// Close logs out the current session by sending DELETE /api/auth.
// Safe to call multiple times or when not authenticated.
func (c *Client) Close() {
	c.mu.RLock()
	sid := c.sid
	c.mu.RUnlock()

	if sid == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/auth", nil)
	if err != nil {
		return
	}
	req.Header.Set("X-FTL-SID", sid)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()

	c.mu.Lock()
	c.sid = ""
	c.mu.Unlock()
}

// DoRaw executes an authenticated request and returns the raw HTTP response.
// The caller is responsible for closing the response body.
// Used for non-JSON endpoints (teleporter export, gravity stream).
//
// Only the transport failure is retried — once headers are in hand the response
// is handed straight to the caller, who streams the body themselves.
func (c *Client) DoRaw(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var resp *http.Response
	err := c.withRetry(ctx, idempotentMethod(method), func() error {
		sid, err := c.ensureAuth(ctx)
		if err != nil {
			return err
		}

		req, err := c.buildRequest(ctx, method, path, sid, body)
		if err != nil {
			return err
		}

		resp, err = c.httpClient.Do(req) //nolint:bodyclose // the caller owns and closes the body
		if err != nil {
			return fmt.Errorf("pi-hole API request failed: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// doRequest executes a single API request without auth retry logic.
func (c *Client) doRequest(ctx context.Context, method, path, sid string, body, result any) error {
	req, err := c.buildRequest(ctx, method, path, sid, body)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pi-hole API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 204 No Content — success with no body (e.g. DELETE).
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response from %s: %w", path, err)
	}

	// Error responses.
	if resp.StatusCode >= 400 {
		return c.parseError(resp.StatusCode, resp.Header, path, respBody)
	}

	// Success — unmarshal if result is provided.
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshalling response from %s: %w", path, err)
		}
	}

	return nil
}

// buildRequest creates an HTTP request with auth header and optional JSON body.
func (c *Client) buildRequest(ctx context.Context, method, path, sid string, body any) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	url := c.baseURL + "/api" + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", path, err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sid != "" {
		req.Header.Set("X-FTL-SID", sid)
	}

	return req, nil
}

// parseError converts an error response into a typed error.
func (c *Client) parseError(statusCode int, header http.Header, path string, body []byte) error {
	apiErr := &APIError{
		StatusCode: statusCode,
		Endpoint:   path,
		RetryAfter: parseRetryAfter(header),
	}

	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Key != "" {
		apiErr.Key = errResp.Error.Key
		apiErr.Message = errResp.Error.Message
		apiErr.Hint = errResp.Error.Hint
	} else {
		apiErr.Key = "unknown"
		apiErr.Message = fmt.Sprintf("HTTP %d", statusCode)
	}

	return classifyError(apiErr)
}

// isAuthError checks if an error is an AuthError.
func isAuthError(err error, target **AuthError) bool {
	return errors.As(err, target)
}
