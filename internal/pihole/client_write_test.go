package pihole

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newAuthedServer returns a test server that answers auth and delegates every
// other request to handler, plus a client pointed at it with retries disabled.
func newAuthedServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" && r.Method == http.MethodPost {
			writeJSON(w, authResponse{Session: sessionInfo{Valid: true, SID: "test-sid"}})
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, New(srv.URL, "test", WithRetry(0, time.Second))
}

func TestClient_Put(t *testing.T) {
	var gotBody map[string]any
	_, c := newAuthedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/domains/deny/exact/ads.example.com" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, map[string]any{"processed": true})
	})

	var result map[string]any
	err := c.Put(context.Background(), "/domains/deny/exact/ads.example.com",
		map[string]any{"comment": "updated"}, &result)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if gotBody["comment"] != "updated" {
		t.Errorf("server saw body %v", gotBody)
	}
	if result["processed"] != true {
		t.Errorf("result = %v", result)
	}
}

func TestClient_PostMultipart(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(backup, []byte("fake-zip-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	var sawFile, sawImport string
	_, c := newAuthedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/teleporter" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-FTL-SID") != "test-sid" {
			t.Error("missing SID header on multipart request")
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer func() { _ = f.Close() }()
		sawFile = hdr.Filename
		sawImport = r.FormValue("import")
		writeJSON(w, map[string]any{"processed": []string{"gravity"}})
	})

	var result map[string]any
	err := c.PostMultipart(context.Background(), "/teleporter", backup,
		map[string]any{"gravity": true}, &result)
	if err != nil {
		t.Fatalf("PostMultipart: %v", err)
	}
	if sawFile != "backup.zip" {
		t.Errorf("uploaded filename = %q", sawFile)
	}
	if !strings.Contains(sawImport, `"gravity":true`) {
		t.Errorf("import options = %q", sawImport)
	}
	if result["processed"] == nil {
		t.Errorf("result = %v", result)
	}
}

func TestClient_PostMultipart_MissingFile(t *testing.T) {
	_, c := newAuthedServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server should never be reached when the file cannot be read")
	})
	err := c.PostMultipart(context.Background(), "/teleporter", "/does/not/exist.zip", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "opening file") {
		t.Fatalf("err = %v, want opening-file error", err)
	}
}

func TestClient_PostMultipart_APIError(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(backup, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, c := newAuthedServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, errorResponse{Error: errorDetail{Key: "bad_request", Message: "invalid archive"}})
	})
	err := c.PostMultipart(context.Background(), "/teleporter", backup, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid archive") {
		t.Fatalf("err = %v, want API error surfaced", err)
	}
}

func TestBuildMultipartBody_NoOptions(t *testing.T) {
	f := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(f, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, contentType, err := buildMultipartBody(f, nil)
	if err != nil {
		t.Fatalf("buildMultipartBody: %v", err)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
		t.Errorf("content type = %q", contentType)
	}
	if !strings.Contains(string(body), "payload") {
		t.Error("body missing file payload")
	}
	if strings.Contains(string(body), `name="import"`) {
		t.Error("body has import field despite nil options")
	}
}

func TestClient_DoRaw(t *testing.T) {
	_, c := newAuthedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-FTL-SID") != "test-sid" {
			t.Error("missing SID header")
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("raw-bytes"))
	})

	resp, err := c.DoRaw(context.Background(), http.MethodGet, "/teleporter", nil)
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Content-Type"); got != "application/zip" {
		t.Errorf("content type = %q", got)
	}
}

func TestClient_Close(t *testing.T) {
	var logoutCalls int
	srv, c := newAuthedServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/auth":
			if r.Header.Get("X-FTL-SID") != "test-sid" {
				t.Error("logout missing SID header")
			}
			logoutCalls++
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/dns/blocking":
			writeJSON(w, BlockingStatus{Blocking: "enabled"})
		default:
			http.NotFound(w, r)
		}
	})
	_ = srv

	// Close before any authentication is a no-op.
	c.Close()
	if logoutCalls != 0 {
		t.Fatal("Close without a session must not call the API")
	}

	// Authenticate, then Close must revoke the session exactly once.
	var status BlockingStatus
	if err := c.Get(context.Background(), "/dns/blocking", &status); err != nil {
		t.Fatalf("Get: %v", err)
	}
	c.Close()
	if logoutCalls != 1 {
		t.Errorf("logout calls = %d, want 1", logoutCalls)
	}
	c.Close() // idempotent: SID already cleared
	if logoutCalls != 1 {
		t.Errorf("logout calls after second Close = %d, want 1", logoutCalls)
	}
}
