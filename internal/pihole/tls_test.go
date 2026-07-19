package pihole

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newSelfSignedServer starts a TLS server with httptest's self-signed
// certificate, answering auth and a single blocking-status endpoint.
func newSelfSignedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth":
			writeJSON(w, authResponse{Session: sessionInfo{Valid: true, SID: "test-sid"}})
		case "/api/dns/blocking":
			writeJSON(w, BlockingStatus{Blocking: "enabled"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_TLSDefault_RejectsSelfSigned(t *testing.T) {
	srv := newSelfSignedServer(t)

	c := New(srv.URL, "test", WithRetry(0, time.Second))

	var status BlockingStatus
	if err := c.Get(context.Background(), "/dns/blocking", &status); err == nil {
		t.Fatal("expected certificate verification error, got nil")
	}
}

func TestClient_TLSSkipVerify_AllowsSelfSigned(t *testing.T) {
	srv := newSelfSignedServer(t)

	c := New(srv.URL, "test", WithRetry(0, time.Second), WithTLSSkipVerify(true))

	var status BlockingStatus
	if err := c.Get(context.Background(), "/dns/blocking", &status); err != nil {
		t.Fatalf("unexpected error with TLS verification disabled: %v", err)
	}
	if status.Blocking != "enabled" {
		t.Errorf("Blocking = %q, want %q", status.Blocking, "enabled")
	}
}

func TestClient_TLSSkipVerifyFalse_LeavesTransportUntouched(t *testing.T) {
	c := New("http://example.invalid", "test", WithTLSSkipVerify(false))
	if c.httpClient.Transport != nil {
		t.Error("WithTLSSkipVerify(false) should not replace the default transport")
	}
}
