package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// newFakeAPI serves auth plus canned JSON per API path.
func newFakeAPI(t *testing.T, routes map[string]string) *pihole.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session": map[string]any{"valid": true, "sid": "test-sid"},
			})
			return
		}
		if body, ok := routes[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"key":"not_found","message":"not found"}}`))
	}))
	t.Cleanup(srv.Close)
	return pihole.New(srv.URL, "test", pihole.WithRetry(0, time.Second))
}

func readRequest(uri string) mcp.ReadResourceRequest {
	var req mcp.ReadResourceRequest
	req.Params.URI = uri
	return req
}

// text extracts the single markdown payload from a handler result.
func text(t *testing.T, contents []mcp.ResourceContents) string {
	t.Helper()
	if len(contents) != 1 {
		t.Fatalf("contents = %d, want 1", len(contents))
	}
	tc, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatalf("contents type %T", contents[0])
	}
	if tc.MIMEType != "text/markdown" {
		t.Errorf("mime = %q", tc.MIMEType)
	}
	return tc.Text
}

func TestStatusResource(t *testing.T) {
	c := newFakeAPI(t, map[string]string{
		"/api/dns/blocking": `{"blocking":"enabled","timer":42.7}`,
		"/api/info/version": `{"version":{"core":{"local":{"version":"v6.1.4"}},"ftl":{"local":{"version":"v6.7"}},"web":{"local":{"version":"v6.3"}}}}`,
	})
	got := text(t, mustRead(t, statusResourceHandler(c), "pihole://status"))
	for _, want := range []string{"**Blocking:** enabled", "**Timer:** 43 seconds", "**FTL:** v6.7", "**Core:** v6.1.4", "**Web:** v6.3"} {
		if !strings.Contains(got, want) {
			t.Errorf("status missing %q in:\n%s", want, got)
		}
	}
}

func TestStatusResource_NoTimerNoVersion(t *testing.T) {
	c := newFakeAPI(t, map[string]string{
		"/api/dns/blocking": `{"blocking":"disabled"}`,
		// /info/version 404s: status must still render (version is optional).
	})
	got := text(t, mustRead(t, statusResourceHandler(c), "pihole://status"))
	if !strings.Contains(got, "**Blocking:** disabled") {
		t.Errorf("status = %q", got)
	}
	if strings.Contains(got, "Timer") || strings.Contains(got, "FTL") {
		t.Errorf("unexpected optional sections in:\n%s", got)
	}
}

func TestStatusResource_Error(t *testing.T) {
	c := newFakeAPI(t, nil) // no routes: blocking status fails
	if _, err := statusResourceHandler(c)(context.Background(), readRequest("pihole://status")); err == nil {
		t.Fatal("expected error when blocking status is unavailable")
	}
}

func TestSummaryResource(t *testing.T) {
	c := newFakeAPI(t, map[string]string{
		"/api/stats/summary": `{"queries":{"total":45231,"blocked":12000,"percent_blocked":26.5,"cached":9000,"forwarded":24000},"clients":{"active":12},"gravity":{"domains_being_blocked":123456}}`,
	})
	got := text(t, mustRead(t, summaryResourceHandler(c), "pihole://summary"))
	for _, want := range []string{"45,231", "12,000 (26.5%)", "**Active clients:** 12", "123,456"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q in:\n%s", want, got)
		}
	}
}

func TestClientResource(t *testing.T) {
	c := newFakeAPI(t, map[string]string{
		"/api/clients/192.168.1.50": `{"clients":[{"client":"192.168.1.50","name":"laptop","comment":"my laptop","groups":[0]}]}`,
	})
	got := text(t, mustReadTemplate(t, clientResourceHandler(c), "pihole://clients/192.168.1.50"))
	if !strings.Contains(got, "# Client: 192.168.1.50") || !strings.Contains(got, "laptop") {
		t.Errorf("client resource:\n%s", got)
	}
}

func TestDomainListResource(t *testing.T) {
	c := newFakeAPI(t, map[string]string{
		"/api/domains/deny/exact": `{"domains":[{"domain":"ads.example.com","enabled":true},{"domain":"tracker.example.com","enabled":false}]}`,
	})
	got := text(t, mustReadTemplate(t, domainListResourceHandler(c), "pihole://domains/deny/exact"))
	for _, want := range []string{"deny/exact (2)", "ads.example.com (enabled: Yes)", "tracker.example.com (enabled: No)"} {
		if !strings.Contains(got, want) {
			t.Errorf("domain list missing %q in:\n%s", want, got)
		}
	}
}

func TestListDetailResource(t *testing.T) {
	// The escaped address decodes to a plain path on the server side.
	c := newFakeAPI(t, map[string]string{
		"/api/lists/https://example.com/ads.txt": `{"lists":[{"address":"https://example.com/ads.txt","type":"block","number":50000,"enabled":true,"comment":"ads"}]}`,
	})
	got := text(t, mustReadTemplate(t, listDetailResourceHandler(c), "pihole://lists/https%3A%2F%2Fexample.com%2Fads.txt"))
	for _, want := range []string{"**Type:** block", "50,000", "**Enabled:** Yes"} {
		if !strings.Contains(got, want) {
			t.Errorf("list detail missing %q in:\n%s", want, got)
		}
	}
}

func TestInstancesResource(t *testing.T) {
	c := newFakeAPI(t, nil)
	_ = c
	reg := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "downstairs", URL: "http://a.invalid", Password: "x"},
		{Name: "upstairs", URL: "http://b.invalid", Password: "x"},
	})
	t.Cleanup(reg.Close)

	got := text(t, mustRead(t, instancesResourceHandler(reg), "pihole://instances"))
	for _, want := range []string{"**2 Pi-hole instances configured:**", "**downstairs** (default)", "pihole://upstairs/status", "instance=all"} {
		if !strings.Contains(got, want) {
			t.Errorf("instances missing %q in:\n%s", want, got)
		}
	}
}

func TestRegisterAll_SingleAndMulti(t *testing.T) {
	single := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "primary", URL: "http://a.invalid", Password: "x"},
	})
	t.Cleanup(single.Close)
	s := server.NewMCPServer("test", "0.0.0", server.WithResourceCapabilities(false, false))
	RegisterAll(s, single) // must not panic

	multi := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "primary", URL: "http://a.invalid", Password: "x"},
		{Name: "secondary", URL: "http://b.invalid", Password: "x"},
	})
	t.Cleanup(multi.Close)
	s2 := server.NewMCPServer("test", "0.0.0", server.WithResourceCapabilities(false, false))
	RegisterAll(s2, multi) // registers per-instance resources; must not panic
}

func mustRead(t *testing.T, h server.ResourceHandlerFunc, uri string) []mcp.ResourceContents {
	t.Helper()
	contents, err := h(context.Background(), readRequest(uri))
	if err != nil {
		t.Fatalf("read %s: %v", uri, err)
	}
	return contents
}

func mustReadTemplate(t *testing.T, h server.ResourceTemplateHandlerFunc, uri string) []mcp.ResourceContents {
	t.Helper()
	contents, err := h(context.Background(), readRequest(uri))
	if err != nil {
		t.Fatalf("read %s: %v", uri, err)
	}
	return contents
}
