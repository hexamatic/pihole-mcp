package pihole

import (
	"bytes"
	"context"
	"crypto/tls"
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
	// restartWait bounds AwaitRestart; zero means restartWaitTimeout.
	restartWait time.Duration
	// locks serialises this server's read-then-write sequences (locks.go).
	locks keyedMutex
	mu    sync.RWMutex
	sid   string
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

// WithTLSSkipVerify disables TLS certificate verification. An explicit
// opt-in for Pi-hole instances serving self-signed certificates; it has
// no effect on plain-HTTP instances.
func WithTLSSkipVerify(skip bool) Option {
	return func(c *Client) {
		if !skip {
			return
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // explicit user opt-in for self-signed certificates
		}
		c.httpClient.Transport = transport
	}
}

// WithHTTPClient sets a custom HTTP client (useful for testing).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// RedirectError is returned when Pi-hole, or something in front of it,
// answers with a redirect.
type RedirectError struct {
	// Location is where the reply pointed, with any credentials hidden.
	Location string
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("pi-hole's address redirected to %s; set PIHOLE_URL to the address it redirects to, "+
		"since this server does not follow redirects", e.Location)
}

// refuseRedirect stops the HTTP client following a redirect. Go copies the
// headers it set to the new address, X-FTL-SID among them, even on another
// host, and a 307 or 308 sends the request body again there, which for a
// login is the password. Pi-hole's API never redirects, so a redirect means
// a proxy or a wrong address, and the fix is to configure the final one.
func refuseRedirect(req *http.Request, _ []*http.Request) error {
	loc := *req.URL
	loc.User = nil
	loc.RawQuery = ""
	return &RedirectError{Location: loc.String()}
}

// send performs one HTTP request. A refused redirect comes back as the bare
// *RedirectError: net/http wraps it in a url.Error whose URL is the raw
// Location header, credentials and all, and that error text reached tool
// replies and -check output.
func send(hc *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := hc.Do(req)
	if err != nil {
		var redirect *RedirectError
		if errors.As(err, &redirect) {
			if resp != nil {
				_ = resp.Body.Close()
			}
			return nil, redirect
		}
	}
	return resp, err
}

// New creates a new Pi-hole API client.
func New(baseURL, password string, opts ...Option) *Client {
	c := &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: password,
		httpClient: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: refuseRedirect,
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
//
// A change request that was sent but got no reply returns an *UncertainError,
// since Pi-hole may already have applied it.
func (c *Client) Do(ctx context.Context, method, path string, body, result any) error {
	live := ctx.Err() == nil
	err := c.do(ctx, method, path, body, result)
	if !idempotentMethod(method) && mayHaveApplied(err, live) {
		return &UncertainError{err}
	}
	return err
}

func (c *Client) do(ctx context.Context, method, path string, body, result any) error {
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
	f, err := os.Open(filePath) //nolint:gosec // file path is user-provided via MCP tool parameter
	if err != nil {
		return fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer func() { _ = f.Close() }()
	return c.PostMultipartReader(ctx, path, filepath.Base(filePath), f, importOptions, result)
}

// PostMultipartReader is PostMultipart for an archive the caller has already
// opened, so the file checked is the file sent. name is the upload's
// filename.
func (c *Client) PostMultipartReader(ctx context.Context, path, name string, file io.Reader, importOptions map[string]any, result any) error {
	body, contentType, err := buildMultipartBody(name, file, importOptions)
	if err != nil {
		return err
	}
	live := ctx.Err() == nil

	err = c.withRetry(ctx, false, func() error {
		sid, err := c.ensureAuth(ctx)
		if err != nil {
			return err
		}

		err = c.sendMultipart(ctx, path, sid, body, contentType, result)
		// A 401 means FTL refused the upload before reading it, typically
		// because the session expired or FTL restarted. Log in again and
		// send it once more, exactly as Do does.
		var authErr *AuthError
		if !errors.As(err, &authErr) {
			return err
		}
		newSID, err := c.handleAuthRetry(ctx, sid)
		if err != nil {
			return err
		}
		return c.sendMultipart(ctx, path, newSID, body, contentType, result)
	})
	if mayHaveApplied(err, live) {
		return &UncertainError{err}
	}
	return err
}

// sendMultipart sends one encoded multipart body and decodes the reply.
func (c *Client) sendMultipart(ctx context.Context, path, sid string, body []byte, contentType string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api"+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating multipart request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-FTL-SID", sid)

	resp, err := send(c.httpClient, req)
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
}

// buildMultipartBody encodes filePath (and any import options) as a multipart
// form, returning the body and its Content-Type.
func buildMultipartBody(name string, file io.Reader, importOptions map[string]any) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", name)
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

	resp, err := send(c.httpClient, req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()

	c.mu.Lock()
	c.sid = ""
	c.mu.Unlock()
}

// maxErrorBody bounds how much of an error response is read. FTL's error
// bodies are a few hundred bytes; anything larger is not one of them.
const maxErrorBody = 1 << 20

// DoRaw executes an authenticated request and returns the raw HTTP response,
// for an endpoint whose body the caller streams itself (teleporter export,
// gravity). The caller closes the body.
//
// The status is checked before the response is handed over. It used not to
// be, so an expired session or a refusal reached the caller as a successful
// response whose body was FTL's JSON error: pihole_teleporter_export saved
// that as "Backup saved", and pihole_instance_sync kept it as its rollback
// point and went on to write to the target. A 401 now refreshes the session
// and replays the request once, as Do does, and any other status of 400 or
// above comes back as the same typed error Do would return.
//
// Only a transport failure is retried. Once a successful response is in hand
// its body belongs to the caller.
func (c *Client) DoRaw(ctx context.Context, method, path string, body any) (*http.Response, error) {
	return c.doRaw(ctx, c.httpClient, method, path, body)
}

// DoStream is DoRaw for a response that streams for as long as the work
// behind it takes, such as a gravity rebuild. The request timeout
// (PIHOLE_REQUEST_TIMEOUT) covers reading the whole body, so it would cut
// such a stream off part way. DoStream drops that limit and leaves ctx as the
// only bound, so the caller must give ctx a deadline or cancel it when the
// stream goes quiet.
func (c *Client) DoStream(ctx context.Context, method, path string, body any) (*http.Response, error) {
	hc := *c.httpClient
	hc.Timeout = 0
	return c.doRaw(ctx, &hc, method, path, body)
}

func (c *Client) doRaw(ctx context.Context, hc *http.Client, method, path string, body any) (*http.Response, error) {
	var resp *http.Response
	err := c.withRetry(ctx, idempotentMethod(method), func() error {
		sid, err := c.ensureAuth(ctx)
		if err != nil {
			return err
		}

		resp, err = c.sendRaw(ctx, hc, method, path, sid, body) //nolint:bodyclose // returned to the caller, who closes it
		var authErr *AuthError
		if !errors.As(err, &authErr) {
			return err
		}
		newSID, err := c.handleAuthRetry(ctx, sid)
		if err != nil {
			return err
		}
		resp, err = c.sendRaw(ctx, hc, method, path, newSID, body) //nolint:bodyclose // returned to the caller, who closes it
		return err
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// sendRaw sends one request and returns the response when its status is below
// 400. Otherwise it reads the error body, closes it, and returns the typed
// error.
func (c *Client) sendRaw(ctx context.Context, hc *http.Client, method, path, sid string, body any) (*http.Response, error) {
	req, err := c.buildRequest(ctx, method, path, sid, body)
	if err != nil {
		return nil, err
	}

	resp, err := send(hc, req) //nolint:bodyclose // closed below on error, otherwise owned and closed by the caller
	if err != nil {
		return nil, fmt.Errorf("pi-hole API request failed: %w", err)
	}
	if resp.StatusCode < 400 {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return nil, c.parseError(resp.StatusCode, resp.Header, path, errBody)
}

// doRequest executes a single API request without auth retry logic.
func (c *Client) doRequest(ctx context.Context, method, path, sid string, body, result any) error {
	req, err := c.buildRequest(ctx, method, path, sid, body)
	if err != nil {
		return err
	}

	resp, err := send(c.httpClient, req)
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
