package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOriginValidator_LoopbackHostAccepted(t *testing.T) {
	v := NewOriginValidator(defaultAllowList())
	h := v.Middleware(okHandler())

	for _, host := range []string{"localhost:9090", "127.0.0.1:9090", "[::1]:9090"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+host+"/mcp", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("host %q should be accepted, got %d", host, rec.Code)
		}
	}
}

func TestOriginValidator_NonLoopbackHostRejected(t *testing.T) {
	v := NewOriginValidator(defaultAllowList())
	h := v.Middleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://pihole.evil.example.com/mcp", nil)
	req.Host = "pihole.evil.example.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback host should be 403, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("missing JSON content type: %q", rec.Header().Get("Content-Type"))
	}
}

func TestOriginValidator_OriginInAllowlist(t *testing.T) {
	v := NewOriginValidator(defaultAllowList())
	h := v.Middleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost:9090/mcp", nil)
	req.Host = "localhost:9090"
	req.Header.Set("Origin", "http://localhost:9090")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("origin in allowlist should pass, got %d", rec.Code)
	}
}

func TestOriginValidator_EvilOriginRejected(t *testing.T) {
	v := NewOriginValidator(defaultAllowList())
	h := v.Middleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost:9090/mcp", nil)
	req.Host = "localhost:9090"
	req.Header.Set("Origin", "http://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("evil Origin should be 403, got %d", rec.Code)
	}
}

func TestOriginValidator_MissingOriginAllowed(t *testing.T) {
	// Non-browser MCP clients (e.g. LibreChat, custom Go clients) don't send Origin.
	v := NewOriginValidator(defaultAllowList())
	h := v.Middleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost:9090/mcp", nil)
	req.Host = "localhost:9090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("missing Origin should pass, got %d", rec.Code)
	}
}

func TestOriginValidator_WildcardDisablesEnforcement(t *testing.T) {
	v := NewOriginValidator([]string{"*"})
	h := v.Middleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://anything.example.com/mcp", nil)
	req.Host = "anything.example.com"
	req.Header.Set("Origin", "http://attacker.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("wildcard should let anything through, got %d", rec.Code)
	}
}

func TestOriginValidator_EmptyAllowlistIsNoop(t *testing.T) {
	// Constructed but empty — defensively a no-op so a misconfig doesn't brick the server.
	v := NewOriginValidator([]string{})
	h := v.Middleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://anything.example.com/mcp", nil)
	req.Host = "anything.example.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty allowlist should be a no-op, got %d", rec.Code)
	}
}

func TestOriginValidator_IPv6Literal(t *testing.T) {
	v := NewOriginValidator([]string{"[::1]"})
	h := v.Middleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://[::1]:9090/mcp", nil)
	req.Host = "[::1]:9090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("IPv6 loopback should be accepted, got %d", rec.Code)
	}
}

func TestOriginValidator_CaseInsensitiveHost(t *testing.T) {
	v := NewOriginValidator([]string{"LOCALHOST"})
	h := v.Middleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost:9090/mcp", nil)
	req.Host = "Localhost:9090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("case-insensitive match should pass, got %d", rec.Code)
	}
}

func TestStripPort(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"localhost:9090", "localhost"},
		{"localhost", "localhost"},
		{"127.0.0.1:80", "127.0.0.1"},
		{"[::1]:9090", "[::1]"},
		{"[::1]", "[::1]"},
		{"[fe80::1]:9090", "[fe80::1]"},
	}
	for _, tt := range tests {
		if got := stripPort(tt.in); got != tt.want {
			t.Errorf("stripPort(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestChainOrder(t *testing.T) {
	// Verify outermost-first wrapping
	var order []string
	mk := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, "in:"+name)
				next.ServeHTTP(w, r)
				order = append(order, "out:"+name)
			})
		}
	}
	chain := Chain(mk("a"), mk("b"), mk("c"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	}))
	chain.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	want := []string{"in:a", "in:b", "in:c", "handler", "out:c", "out:b", "out:a"}
	if len(order) != len(want) {
		t.Fatalf("chain order length mismatch: got %v want %v", order, want)
	}
	for i, s := range want {
		if order[i] != s {
			t.Errorf("position %d: got %q, want %q", i, order[i], s)
		}
	}
}

func defaultAllowList() []string {
	return []string{"localhost", "127.0.0.1", "[::1]"}
}
