package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startTimeoutServer runs handler behind newHTTPServer with a short read
// timeout.
func startTimeoutServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	prev := httpReadTimeout
	httpReadTimeout = 300 * time.Millisecond
	t.Cleanup(func() { httpReadTimeout = prev })

	ts := httptest.NewUnstartedServer(handler)
	ts.Config = newHTTPServer("", handler)
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

func TestHTTPServer_CutsASlowRequestBody(t *testing.T) {
	// A body sent a byte at a time held the connection open for as long as
	// the client liked, before any authentication could refuse it.
	ts := startTimeoutServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	}))

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 1000\r\n\r\n"); err != nil {
		t.Fatal(err)
	}

	closed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, conn) // returns when the server closes
		close(closed)
	}()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-closed:
			return
		case <-deadline:
			t.Fatal("the server was still reading a trickled body after 3s")
		case <-time.After(100 * time.Millisecond):
			_, _ = conn.Write([]byte("x"))
		}
	}
}

func TestHTTPServer_LongResponsesOutliveTheReadTimeout(t *testing.T) {
	// SSE streams stay open far longer than the read timeout. Go clears the
	// read deadline once the request is read, which is what this relies on.
	ts := startTimeoutServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(900 * time.Millisecond) // three read timeouts
		_, _ = fmt.Fprint(w, "data: second\n\n")
	}))

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var lines []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if l := sc.Text(); l != "" {
			lines = append(lines, l)
		}
	}
	if got := strings.Join(lines, ","); got != "data: first,data: second" {
		t.Errorf("stream = %q (%v), want both events", got, sc.Err())
	}
}
