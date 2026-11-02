package pihole

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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
	file, err := os.Open(f) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	body, contentType, err := buildMultipartBody(filepath.Base(f), file, nil)
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

// expiringSessionServer issues sid-1, sid-2, ... and answers 401 to any
// request carrying sid-1, as FTL does once it has forgotten a session.
func expiringSessionServer(t *testing.T, handler http.HandlerFunc) (*Client, *int) {
	t.Helper()
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" && r.Method == http.MethodPost {
			logins++
			writeJSON(w, authResponse{Session: sessionInfo{Valid: true, SID: "sid-" + strconv.Itoa(logins)}})
			return
		}
		if r.Header.Get("X-FTL-SID") == "sid-1" {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, errorResponse{Error: errorDetail{Key: "unauthorized", Message: "Unauthorized"}})
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "test", WithRetry(0, time.Second)), &logins
}

func TestClient_DoRaw_ErrorStatusIsATypedError(t *testing.T) {
	cases := []struct {
		status int
		check  func(error) bool
	}{
		{http.StatusInternalServerError, func(err error) bool { var e *APIError; return errors.As(err, &e) && e.StatusCode == 500 }},
		{http.StatusForbidden, func(err error) bool { var e *ForbiddenError; return errors.As(err, &e) && e.AppSudoRequired() }},
		{http.StatusNotFound, func(err error) bool { var e *NotFoundError; return errors.As(err, &e) }},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			_, c := newAuthedServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				writeJSON(w, errorResponse{Error: errorDetail{Key: "x", Message: "refused",
					Hint: "The current app session is not allowed to export Pi-hole config settings (webserver.api.app_sudo is false)"}})
			})
			resp, err := c.DoRaw(context.Background(), http.MethodGet, "/teleporter", nil)
			if err == nil {
				_ = resp.Body.Close()
				t.Fatalf("DoRaw returned a %d response as success", tc.status)
			}
			if !tc.check(err) {
				t.Errorf("DoRaw error = %T %v, not the typed error for %d", err, err, tc.status)
			}
		})
	}
}

func TestClient_DoRaw_LogsInAgainAfter401(t *testing.T) {
	c, logins := expiringSessionServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("PK\x03\x04archive"))
	})
	resp, err := c.DoRaw(context.Background(), http.MethodGet, "/teleporter", nil)
	if err != nil {
		t.Fatalf("DoRaw after an expired session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "PK\x03\x04archive" {
		t.Errorf("body = %q, want the replayed request's archive", body)
	}
	if *logins != 2 {
		t.Errorf("logins = %d, want 2", *logins)
	}
}

func TestClient_PostMultipart_LogsInAgainAfter401(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(backup, []byte("PK\x03\x04data"), 0o600); err != nil {
		t.Fatal(err)
	}
	uploads := 0
	c, logins := expiringSessionServer(t, func(w http.ResponseWriter, _ *http.Request) {
		uploads++
		writeJSON(w, map[string]any{"files": []string{"etc/pihole/pihole.toml"}})
	})
	var result TeleporterImportResponse
	if err := c.PostMultipart(context.Background(), "/teleporter", backup, nil, &result); err != nil {
		t.Fatalf("PostMultipart after an expired session: %v", err)
	}
	if *logins != 2 || uploads != 1 {
		t.Errorf("logins = %d, uploads reaching the handler = %d; want 2 and 1", *logins, uploads)
	}
	if got := result.ImportedFiles(); len(got) != 1 {
		t.Errorf("ImportedFiles = %v", got)
	}
}

func TestClient_DoesNotFollowRedirects(t *testing.T) {
	// Go copies X-FTL-SID to wherever a redirect points, and a 307 sends a
	// login's password there again.
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere.Add(1)
	}))
	t.Cleanup(other.Close)
	// The target carries credentials, which net/http's own error quoted.
	target := strings.Replace(other.URL, "http://", "http://user:hunter2@", 1) + "/api/elsewhere"
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(front.Close)

	err := New(front.URL, "secret", WithRetry(0, time.Second)).Get(context.Background(), "/info/version", nil)
	var redirect *RedirectError
	if !errors.As(err, &redirect) || !strings.Contains(err.Error(), "set PIHOLE_URL") {
		t.Fatalf("err = %T %v, want a RedirectError telling the user what to set", err, err)
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("the redirect target received %d request(s)", n)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the error shows the redirect's credentials: %v", err)
	}
}
