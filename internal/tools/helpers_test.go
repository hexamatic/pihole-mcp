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
	"github.com/hexamatic/pihole-mcp/internal/pihole/wiretest"
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

// piholeHandler returns a recorder that routes API requests by path and keeps
// every request it saw, so a test can assert on the wire as well as on the
// reply. Routes are keyed by path ("/dns/blocking") or by method and path
// ("PUT /dns/blocking", which takes precedence), and map to any
// JSON-serialisable value. The /api prefix is stripped automatically. Auth
// requests are handled transparently.
//
// Route keys are matched against the DECODED path, so a key covering a value
// with reserved characters carries them literally ("/config/dns/upstreams/1.1.1.1#53",
// not "%23"). The selectors take either spelling; AssertRawPath is what pins
// the escaping that actually went onto the wire.
func piholeHandler(routes map[string]any) *recorder {
	rt := newRouteTable(routes)
	return recordingProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			writeTestJSON(w, map[string]any{
				"session": map[string]any{"valid": true, "sid": "test-sid"},
			})
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api")
		if resp, ok := rt.lookup(r.Method, path); ok {
			writeTestJSON(w, resp)
			return
		}
		writeNoRouteError(w, r)
	}))
}

// piholeRawHandler routes API requests and returns raw bytes for non-JSON
// endpoints, recording every request as piholeHandler does. Use textRoutes for
// endpoints that return plain text (e.g. gravity update). Both tables accept
// "METHOD /path" keys, which take precedence over a bare path.
func piholeRawHandler(jsonRoutes map[string]any, textRoutes map[string]string) *recorder {
	jsonTable := newRouteTable(jsonRoutes)
	textTable := newRouteTable(textRoutes)
	return recordingProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			writeTestJSON(w, map[string]any{
				"session": map[string]any{"valid": true, "sid": "test-sid"},
			})
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api")
		if resp, ok := jsonTable.lookup(r.Method, path); ok {
			writeTestJSON(w, resp)
			return
		}
		if text, ok := textTable.lookup(r.Method, path); ok {
			w.Header().Set("Content-Type", "text/plain")
			//nolint:gosec // G705: the text is a literal from the test's own route table, never from the request
			_, _ = fmt.Fprint(w, text)
			return
		}
		writeNoRouteError(w, r)
	}))
}

// piholeErrorServer returns a recorder that responds with a Pi-hole API error.
func piholeErrorServer(statusCode int, key, message, hint string) *recorder {
	return recordingProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeNoRouteError answers a request the route table does not cover with a
// Pi-hole-shaped 404 naming the method and the target it actually saw.
//
// A handler that addresses the wrong endpoint fails here, in callTool, before
// any wire assertion gets to run, so this message is the only one the
// maintainer reads. net/http's stock "404 page not found" is plain text, which
// the client cannot parse, leaving the tool to report a bare "The requested
// resource does not exist" that names neither the verb nor the path. Naming
// both turns a bisect into a glance.
func writeNoRouteError(w http.ResponseWriter, r *http.Request) {
	rawPath, rawQuery := wiretest.SplitRequestTarget(r)
	target := strings.TrimPrefix(rawPath, "/api")
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"key": "not_found",
			"message": fmt.Sprintf("no route in the test fake for %s %s",
				r.Method, target),
			"hint": "the handler addressed an endpoint this test did not set up; " +
				"either the route table is incomplete or the handler is addressing the wrong endpoint",
		},
	})
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

// ---------------------------------------------------------------------------
// Request recording
//
// The recorder itself lives in internal/pihole/wiretest so the reconciliation
// writes in internal/pihole/sync.go can be graded by the same assertions. The
// alias below keeps this package's long-standing unexported spelling, so no
// call site had to change when it moved. Captured requests are referred to by
// inference (rec.Only returns a wiretest.Request), so no second alias is needed.
// ---------------------------------------------------------------------------

type recorder = wiretest.Recorder

func recordingProxy(inner http.Handler) *recorder { return wiretest.Proxy(inner) }

// ---------------------------------------------------------------------------
// Route tables
// ---------------------------------------------------------------------------

// routeTable resolves a request to a canned response. Keys are either a bare
// path ("/dns/blocking") or a method-qualified path ("PUT /dns/blocking"). The
// method-qualified key wins, so one handler can answer a GET and a PUT of the
// same path differently, which is what a write tool needs when it reads its own
// change back.
type routeTable[T any] struct {
	byMethodPath map[string]T
	byPath       map[string]T
}

func newRouteTable[T any](routes map[string]T) routeTable[T] {
	rt := routeTable[T]{
		byMethodPath: make(map[string]T, len(routes)),
		byPath:       make(map[string]T, len(routes)),
	}
	for key, v := range routes {
		if method, path, ok := splitRouteKey(key); ok {
			rt.byMethodPath[method+" "+path] = v
			continue
		}
		rt.byPath[key] = v
	}
	return rt
}

func (rt routeTable[T]) lookup(method, path string) (T, bool) {
	if v, ok := rt.byMethodPath[strings.ToUpper(method)+" "+path]; ok {
		return v, true
	}
	v, ok := rt.byPath[path]
	return v, ok
}

// splitRouteKey splits "PUT /dns/blocking" into its method and path. A key that
// is a bare path is left alone, including the unlikely one containing a space.
func splitRouteKey(key string) (method, path string, ok bool) {
	m, p, found := strings.Cut(key, " ")
	if !found || m == "" || !strings.HasPrefix(p, "/") || strings.Contains(p, " ") {
		return "", "", false
	}
	return strings.ToUpper(m), p, true
}
