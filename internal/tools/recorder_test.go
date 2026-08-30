package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole/wiretest"
)

// The recorder is the only thing standing between this package and another
// pihole_config_set: a tool that sends the wrong method, query or body while
// every test stays green. These tests pin the behaviour the wire assertions
// depend on, because a harness that quietly agrees with everything is worse
// than no harness at all.

// serve pushes one request through a recorder without a real client, so the
// mechanics can be checked without the /api/auth round trip.
func serve(t *testing.T, rec *recorder, method, target, body string) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequestWithContext(t.Context(), method, target, nil)
	} else {
		r = httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	}
	rec.ServeHTTP(httptest.NewRecorder(), r)
}

func TestRecorder_CapturesTheWholeRequest(t *testing.T) {
	rec := piholeHandler(map[string]any{"/domains/deny/exact/example.com": map[string]any{"ok": true}})
	serve(t, rec, "PUT", "/api/domains/deny/exact/example.com?comment=blocked&type=exact",
		`{"comment":"blocked","enabled":false}`)

	req := rec.Only(t, "PUT", "/domains/deny/exact/example.com")
	if req.Method != "PUT" {
		t.Errorf("Method = %q, want PUT", req.Method)
	}
	req.AssertRawPath(t, "/domains/deny/exact/example.com")
	req.AssertQuery(t, "comment", "blocked")
	req.AssertQuery(t, "type", "exact")
	req.AssertNoQuery(t, "restart")
	req.AssertBodyKeys(t, "comment", "enabled")
	req.AssertField(t, "enabled", false)
	if req.RawQuery != "comment=blocked&type=exact" {
		t.Errorf("RawQuery = %q, want the query exactly as sent", req.RawQuery)
	}
	if got, ok := req.Field(t, "comment"); !ok || got != "blocked" {
		t.Errorf("Field(comment) = %v, %v; want blocked, true", got, ok)
	}
}

// A route table keyed by path alone cannot express "the GET answers one thing
// and the PUT another", which is exactly what a write tool that reads its own
// change back needs.
func TestRecorder_MethodQualifiedRouteWins(t *testing.T) {
	rec := piholeRawHandler(map[string]any{
		"/dns/blocking":     map[string]any{"blocking": "enabled"},
		"PUT /dns/blocking": map[string]any{"blocking": "disabled"},
	}, nil)

	get := httptest.NewRecorder()
	rec.ServeHTTP(get, httptest.NewRequestWithContext(t.Context(), "GET", "/api/dns/blocking", nil))
	if !strings.Contains(get.Body.String(), "enabled") || strings.Contains(get.Body.String(), "disabled") {
		t.Errorf("GET answered %q, want the bare-path route", get.Body.String())
	}

	put := httptest.NewRecorder()
	rec.ServeHTTP(put, httptest.NewRequestWithContext(t.Context(), "PUT", "/api/dns/blocking", strings.NewReader(`{}`)))
	if !strings.Contains(put.Body.String(), "disabled") {
		t.Errorf("PUT answered %q, want the method-qualified route", put.Body.String())
	}

	rec.AssertCount(t, "", "/dns/blocking", 2)
	rec.AssertCount(t, "GET", "/dns/blocking", 1)
	if n := rec.Count("PUT", "/dns/blocking"); n != 1 {
		t.Errorf("Count(PUT) = %d, want 1", n)
	}
}

// Route keys that are a bare path must keep working, including the unlikely
// path that contains a space.
func TestRecorder_BareRouteKeysAreUnchanged(t *testing.T) {
	rec := piholeHandler(map[string]any{"/stats/summary": map[string]any{"ok": true}})
	w := httptest.NewRecorder()
	rec.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", "/api/stats/summary", nil))
	if !strings.Contains(w.Body.String(), "ok") {
		t.Errorf("bare path route did not answer, got %q", w.Body.String())
	}

	if _, _, ok := splitRouteKey("/domains/deny/exact/a b"); ok {
		t.Error("a bare path containing a space was mistaken for a method-qualified key")
	}
	if method, path, ok := splitRouteKey("delete /config/dns.hosts/entry"); !ok || method != "DELETE" || path != "/config/dns.hosts/entry" {
		t.Errorf("splitRouteKey = %q, %q, %v; want DELETE, /config/dns.hosts/entry, true", method, path, ok)
	}
}

// The path a handler builds is not the path that goes onto the wire once
// reserved characters are involved, and that difference is a live bug class in
// this package. RawPath must survive net/url's decoding.
func TestRecorder_RawPathPreservesEscaping(t *testing.T) {
	rec := piholeHandler(nil)
	serve(t, rec, "DELETE", "/api/domains/deny/exact/ex%20ample.com", "")

	req := rec.Last(t, "DELETE", "")
	req.AssertRawPath(t, "/domains/deny/exact/ex%20ample.com")
	if req.Path != "/domains/deny/exact/ex ample.com" {
		t.Errorf("Path = %q, want the percent-decoded form", req.Path)
	}
}

// Every test makes an auth request and no test is about it, so it is recorded
// but never selected.
func TestRecorder_AuthIsRecordedButNeverSelected(t *testing.T) {
	rec := piholeHandler(map[string]any{"/dns/blocking": map[string]any{"blocking": "enabled"}})
	c := newTestClient(t, rec)

	var out map[string]any
	if err := c.Get(context.Background(), "/dns/blocking", &out); err != nil {
		t.Fatalf("get: %v", err)
	}

	if n := len(rec.AllRequests()); n != 2 {
		t.Errorf("AllRequests() has %d requests, want the login plus the call", n)
	}
	if n := len(rec.Requests()); n != 1 {
		t.Errorf("Requests() has %d requests, want the call alone", n)
	}
	rec.AssertNone(t, "POST", "/auth")
	rec.AssertNone(t, "", "/auth")
	if got := rec.Only(t, "GET", "/dns/blocking"); got.Header.Get("X-FTL-SID") != "test-sid" {
		t.Errorf("session header = %q, want test-sid", got.Header.Get("X-FTL-SID"))
	}
}

// The suite runs under -race and a multi-instance tool fans out concurrently.
func TestRecorder_IsSafeUnderConcurrentRequests(t *testing.T) {
	rec := piholeHandler(map[string]any{"/stats/summary": map[string]any{"ok": true}})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			serve(t, rec, "GET", "/api/stats/summary", "")
		}()
	}
	wg.Wait()
	rec.AssertCount(t, "GET", "/stats/summary", 16)
}

// A route handler that decodes the body must still see it after the recorder
// has read it.
func TestRecorder_BodyStaysReadableForTheRouteHandler(t *testing.T) {
	var seen string
	rec := recordingProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		seen = string(buf[:n])
		writeTestJSON(w, map[string]any{"ok": true})
	}))
	serve(t, rec, "POST", "/api/lists", `{"address":"https://example.com/list.txt"}`)

	if seen != `{"address":"https://example.com/list.txt"}` {
		t.Errorf("route handler read %q, want the full body", seen)
	}
	rec.Only(t, "POST", "/lists").AssertField(t, "address", "https://example.com/list.txt")
}

// encoding/json decodes every number to float64. An assertion written with an
// untyped int literal must still match, or every future numeric wire assertion
// is a trap that passes against both the broken and the fixed handler.
func TestRecorder_NumbersCompareAcrossGoTypes(t *testing.T) {
	rec := piholeHandler(map[string]any{"PATCH /config": map[string]any{"config": map[string]any{}}})
	serve(t, rec, "PATCH", "/api/config", `{"config":{"dns":{"cache":{"size":10001}}}}`)
	req := rec.Only(t, "PATCH", "/config")

	req.AssertField(t, "config.dns.cache.size", 10001)
	req.AssertField(t, "config.dns.cache.size", int64(10001))
	req.AssertField(t, "config.dns.cache.size", 10001.0)

	for _, tc := range []struct {
		name string
		got  any
		want any
		eq   bool
	}{
		{"int against float64", float64(10001), 10001, true},
		{"int64 against float64", float64(10001), int64(10001), true},
		{"uint against float64", float64(7), uint(7), true},
		{"different numbers", float64(10001), 10002, false},
		{"number against string", float64(10001), "10001", false},
		{"string against string", "block", "block", true},
		{"bool against bool", false, false, true},
		{"false against absent-looking nil", false, nil, false},
		{"nested object", map[string]any{"a": float64(1)}, map[string]any{"a": 1}, true},
		{"nested object with an extra key", map[string]any{"a": float64(1), "b": float64(2)}, map[string]any{"a": 1}, false},
		{"array", []any{float64(1), "x"}, []any{1, "x"}, true},
		{"array of a different length", []any{float64(1)}, []any{1, "x"}, false},
	} {
		if got := wiretest.JSONEqual(tc.got, tc.want); got != tc.eq {
			t.Errorf("%s: wiretest.JSONEqual(%#v, %#v) = %v, want %v", tc.name, tc.got, tc.want, got, tc.eq)
		}
	}
}

// "Sent null" and "did not send" are different requests, and Pi-hole treats
// them differently, so the lookup must separate them.
func TestRecorder_AbsentAndNullFieldsAreDifferent(t *testing.T) {
	body := map[string]any{
		"comment": nil,
		"groups":  []any{float64(0)},
		"nested":  map[string]any{"enabled": false},
	}

	if v, ok := wiretest.LookupField(body, "comment"); !ok || v != nil {
		t.Errorf("wiretest.LookupField(comment) = %v, %v; want nil, true (present and null)", v, ok)
	}
	if v, ok := wiretest.LookupField(body, "enabled"); ok {
		t.Errorf("wiretest.LookupField(enabled) = %v, true; want not found", v)
	}
	if v, ok := wiretest.LookupField(body, "nested.enabled"); !ok || v != false {
		t.Errorf("wiretest.LookupField(nested.enabled) = %v, %v; want false, true", v, ok)
	}
	if v, ok := wiretest.LookupField(body, "groups.0"); ok {
		t.Errorf("wiretest.LookupField(groups.0) = %v, true; want not found, arrays are not walked", v)
	}
	if v, ok := wiretest.LookupField(body, "comment.deeper"); ok {
		t.Errorf("wiretest.LookupField(comment.deeper) = %v, true; want not found", v)
	}
}

// The failure message is the product: an assertion that fails without showing
// the wire costs more time than it saves.
func TestRecorder_FailureMessagesShowTheWire(t *testing.T) {
	rec := piholeHandler(map[string]any{"PUT /lists/x": map[string]any{"ok": true}})
	serve(t, rec, "PUT", "/api/lists/x?type=block", `{"comment":"nightly"}`)
	req := rec.Only(t, "PUT", "/lists/x")

	for _, want := range []string{"PUT", "/lists/x?type=block", "type=block", `"comment": "nightly"`} {
		if !strings.Contains(req.Describe(), want) {
			t.Errorf("describe() does not mention %q:\n%s", want, req.Describe())
		}
	}

	dump := rec.Dump()
	if !strings.Contains(dump, "1 request(s) recorded") || !strings.Contains(dump, "PUT /lists/x?type=block") {
		t.Errorf("dump() does not show what was sent:\n%s", dump)
	}
	if empty := recordingProxy(http.NotFoundHandler()).Dump(); !strings.Contains(empty, "no requests were recorded") {
		t.Errorf("dump() of an idle recorder = %q", empty)
	}
	if got := wiretest.SelectorLabel("", ""); got != "<any request>" {
		t.Errorf("wiretest.SelectorLabel(\"\", \"\") = %q", got)
	}
	if got := wiretest.SelectorLabel("put", ""); got != "PUT <any path>" {
		t.Errorf("wiretest.SelectorLabel(put, \"\") = %q", got)
	}
	if got := wiretest.SelectorLabel("", "/lists/x"); got != "<any method> /lists/x" {
		t.Errorf("wiretest.SelectorLabel(\"\", /lists/x) = %q", got)
	}
}

// A body that is not JSON, or is JSON but not an object, must be reported as
// such rather than silently reading as "every field is missing".
func TestRecorder_NonObjectBodiesAreReportedVerbatim(t *testing.T) {
	if got := wiretest.PrettyBody(nil); got != "(empty)" {
		t.Errorf("wiretest.PrettyBody(nil) = %q", got)
	}
	if got := wiretest.PrettyBody([]byte("not json")); !strings.Contains(got, "not valid JSON") {
		t.Errorf("wiretest.PrettyBody(not json) = %q", got)
	}
	if got := wiretest.PrettyBody([]byte(`{"b":1,"a":2}`)); !strings.HasPrefix(strings.TrimSpace(got), `{`) ||
		strings.Index(got, `"b"`) > strings.Index(got, `"a"`) {
		t.Errorf("prettyBody reordered the keys the handler sent: %q", got)
	}
}

// The absence assertions are what the CRUD write tests lean on: a DELETE that
// quietly grew a body, or a stray ?type= copied across from /lists, is
// invisible in a tool's reply text. AssertRawBody covers the batch-delete
// bodies, which are pre-encoded arrays rather than objects and so cannot be
// checked with AssertBodyKeys.
func TestRecorder_AbsenceAndRawBodyAssertions(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"DELETE /domains/deny/exact/example.com": map[string]any{"ok": true},
		"POST /domains:batchDelete":              map[string]any{"ok": true},
	})
	serve(t, rec, "DELETE", "/api/domains/deny/exact/example.com", "")
	serve(t, rec, "POST", "/api/domains:batchDelete",
		`[{"item":"a.com","type":"deny","kind":"exact"}]`)

	del := rec.Only(t, "DELETE", "/domains/deny/exact/example.com")
	del.AssertNoQueryString(t)
	del.AssertNoBody(t)

	batch := rec.Only(t, "POST", "/domains:batchDelete")
	batch.AssertNoQueryString(t)
	batch.AssertRawBody(t, `[{"item":"a.com","type":"deny","kind":"exact"}]`)
}
