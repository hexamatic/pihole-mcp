package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// newTestClient creates a pihole.Client pointing at the test HTTP server.
func newTestClient(t *testing.T, handler http.Handler) *pihole.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return pihole.New(srv.URL, "test", pihole.WithHTTPClient(srv.Client()))
}

// piholeHandler returns an http.HandlerFunc that routes API requests by path.
// Routes are keyed by path (e.g. "/dns/blocking") and map to any JSON-serialisable value.
// The /api prefix is stripped automatically. Auth requests are handled transparently.
func piholeHandler(routes map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			writeTestJSON(w, map[string]any{
				"session": map[string]any{"valid": true, "sid": "test-sid"},
			})
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api")
		if resp, ok := routes[path]; ok {
			writeTestJSON(w, resp)
			return
		}
		http.NotFound(w, r)
	}
}

// piholeRawHandler routes API requests and returns raw bytes for non-JSON endpoints.
// Use textRoutes for endpoints that return plain text (e.g. gravity update).
func piholeRawHandler(jsonRoutes map[string]any, textRoutes map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			writeTestJSON(w, map[string]any{
				"session": map[string]any{"valid": true, "sid": "test-sid"},
			})
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api")
		if resp, ok := jsonRoutes[path]; ok {
			writeTestJSON(w, resp)
			return
		}
		if text, ok := textRoutes[path]; ok {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprint(w, text)
			return
		}
		http.NotFound(w, r)
	}
}

// piholeErrorServer returns a handler that responds with a Pi-hole API error.
func piholeErrorServer(statusCode int, key, message, hint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			writeTestJSON(w, map[string]any{
				"session": map[string]any{"valid": true, "sid": "test-sid"},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"key":     key,
				"message": message,
				"hint":    hint,
			},
		})
	}
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// callTool invokes a handler function directly and returns the text content.
// Fatals on handler error or tool error. The client is wrapped in a
// single-instance registry so existing call sites need no changes.
func callTool(t *testing.T, handlerFn func(*pihole.Registry) server.ToolHandlerFunc, c *pihole.Client, args map[string]any) string {
	t.Helper()
	h := handlerFn(pihole.SingleRegistry(c))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %v", result.Content)
	}
	if len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// callToolExpectError invokes a handler and asserts the result is an error.
// Returns the error text.
func callToolExpectError(t *testing.T, handlerFn func(*pihole.Registry) server.ToolHandlerFunc, c *pihole.Client, args map[string]any) string {
	t.Helper()
	h := handlerFn(pihole.SingleRegistry(c))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error but got success")
	}
	if len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}
