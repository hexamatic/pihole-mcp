// Package wiretest records the HTTP requests a Pi-hole client puts on the
// wire, so a test can assert on what was actually sent and not only on what
// came back.
//
// It exists as its own package rather than a test helper because the request
// builders worth asserting on live in two places: the tool handlers in
// internal/tools and the reconciliation writes in internal/pihole/sync.go. A
// helper confined to one package's _test.go files can only ever grade half of
// them, and the half it cannot see is the half that writes to every configured
// Pi-hole at once.
//
// Nothing in the shipped binary imports this package.
package wiretest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Request recording
//
// The route tables above answer on path alone, so for most of this package's
// history a handler could send the wrong method, the wrong query string or a
// completely wrong body and every test still passed. That is how
// pihole_config_set shipped a body the API rejects from v0.1.0 to v0.8.0. The
// Recorder below keeps every request the fake API saw so a test can assert on
// what went onto the wire, not only on what came back.
// ---------------------------------------------------------------------------

// Request is one HTTP request the fake Pi-hole API observed, captured
// before any routing decision, so a test can assert on what the handler put on
// the wire rather than only on what came back.
//
// Every field is a snapshot taken at capture time and is never written again,
// so reading one from another goroutine is safe. Do not mutate them.
type Request struct {
	Method   string     // GET, PUT, POST, PATCH, DELETE
	Path     string     // percent-decoded path, /api prefix stripped
	RawPath  string     // escaped path exactly as sent, /api prefix stripped
	Query    url.Values // parsed query parameters
	RawQuery string     // query string exactly as sent
	Header   http.Header
	Body     []byte
}

// Recorder answers from a route table and records every request it sees.
// It implements http.Handler, so it drops straight into newTestClient and
// twoInstanceRegistry wherever an http.HandlerFunc used to go.
type Recorder struct {
	inner http.Handler

	mu   sync.Mutex
	seen []Request
}

// authPath is the login endpoint every client hits lazily before its first real
// call. It is recorded, but excluded from every selector: no test is about it.
const authPath = "/auth"

// Proxy wraps an arbitrary handler so the requests reaching it are
// recorded. Use it for the handful of fakes that cannot be expressed as a route
// table, such as one that writes a deliberately malformed response body.
func Proxy(inner http.Handler) *Recorder {
	return &Recorder{inner: inner}
}

// ServeHTTP records the request and then hands it to the wrapped handler. The
// body is read in full and replaced with an equivalent reader first, so route
// handlers that decode it still work.
func (rec *Recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Recorder: reading request body: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
	}

	rawPath, rawQuery := SplitRequestTarget(r)
	rec.mu.Lock()
	rec.seen = append(rec.seen, Request{
		Method:   r.Method,
		Path:     strings.TrimPrefix(r.URL.Path, "/api"),
		RawPath:  strings.TrimPrefix(rawPath, "/api"),
		Query:    r.URL.Query(),
		RawQuery: rawQuery,
		Header:   r.Header.Clone(),
		Body:     body,
	})
	rec.mu.Unlock()

	rec.inner.ServeHTTP(w, r)
}

// SplitRequestTarget recovers the request target exactly as it was sent, before
// net/url decoded it. That is the only way to tell a handler that escaped a
// path segment from one that did not, which several Pi-hole endpoints need
// because list addresses and regex domains carry reserved characters.
func SplitRequestTarget(r *http.Request) (rawPath, rawQuery string) {
	target := r.RequestURI
	if target == "" {
		// ServeHTTP called directly rather than through net/http.
		target = r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
	}
	if i := strings.IndexByte(target, '?'); i >= 0 {
		return target[:i], target[i+1:]
	}
	return target, ""
}

// AllRequests returns every recorded request, including the /api/auth login.
func (rec *Recorder) AllRequests() []Request {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]Request, len(rec.seen))
	copy(out, rec.seen)
	return out
}

// Requests returns the recorded requests other than the /api/auth login.
func (rec *Recorder) Requests() []Request {
	return rec.Select("", "")
}

// Select returns the recorded requests matching a method and a path. An empty
// method matches any method and an empty path matches any path, which is how a
// test asks for "the single PUT, whatever path it went to". The path is
// compared against both the decoded and the raw form, so a selector written
// either way finds the request; use AssertRawPath to pin the exact escaping.
// The /api/auth login is never returned.
func (rec *Recorder) Select(method, path string) []Request {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]Request, 0, len(rec.seen))
	for _, r := range rec.seen {
		if r.Path == authPath {
			continue
		}
		if r.matches(method, path) {
			out = append(out, r)
		}
	}
	return out
}

// Count returns how many recorded requests match a method and a path.
func (rec *Recorder) Count(method, path string) int {
	return len(rec.Select(method, path))
}

// Only returns the single request matching a method and a path, and fails the
// test if there is not exactly one.
func (rec *Recorder) Only(t *testing.T, method, path string) Request {
	t.Helper()
	got := rec.Select(method, path)
	if len(got) == 1 {
		return got[0]
	}
	t.Fatalf("expected exactly one request matching %s, found %d.\n%s",
		SelectorLabel(method, path), len(got), rec.Dump())
	return Request{}
}

// Last returns the most recent request matching a method and a path, and fails
// the test if there is none.
func (rec *Recorder) Last(t *testing.T, method, path string) Request {
	t.Helper()
	got := rec.Select(method, path)
	if len(got) == 0 {
		t.Fatalf("expected at least one request matching %s, found none.\n%s",
			SelectorLabel(method, path), rec.Dump())
		return Request{}
	}
	return got[len(got)-1]
}

// AssertNone fails the test if any request matched a method and a path.
func (rec *Recorder) AssertNone(t *testing.T, method, path string) {
	t.Helper()
	if got := rec.Select(method, path); len(got) != 0 {
		t.Errorf("expected no request matching %s, found %d.\n%s",
			SelectorLabel(method, path), len(got), rec.Dump())
	}
}

// AssertCount fails the test unless exactly want requests matched a method and
// a path.
func (rec *Recorder) AssertCount(t *testing.T, method, path string, want int) {
	t.Helper()
	if got := rec.Count(method, path); got != want {
		t.Errorf("expected %d request(s) matching %s, found %d.\n%s",
			want, SelectorLabel(method, path), got, rec.Dump())
	}
}

// Dump renders every request the fake API saw. Every Recorder failure ends with
// it: a wire assertion that fails without showing the wire costs more time than
// it saves.
func (rec *Recorder) Dump() string {
	reqs := rec.Requests()
	if len(reqs) == 0 {
		return "no requests were recorded (the /api/auth login is not counted)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d request(s) recorded (the /api/auth login is not counted):", len(reqs))
	for i, r := range reqs {
		fmt.Fprintf(&b, "\n  [%d] %s", i, r.Describe())
	}
	return b.String()
}

// matches reports whether the request satisfies a selector. An empty method or
// path matches anything.
func (r Request) matches(method, path string) bool {
	if method != "" && !strings.EqualFold(r.Method, method) {
		return false
	}
	if path != "" && path != r.Path && path != r.RawPath {
		return false
	}
	return true
}

// Describe renders the request the way a failure message needs to see it:
// method, raw path, query and pretty-printed body.
func (r Request) Describe() string {
	target := r.RawPath
	if r.RawQuery != "" {
		target += "?" + r.RawQuery
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", r.Method, target)
	if r.Path != r.RawPath {
		fmt.Fprintf(&b, "\n      decoded path: %s", r.Path)
	}
	if len(r.Query) == 0 {
		b.WriteString("\n      query: (none)")
	} else {
		keys := slices.Sorted(maps.Keys(r.Query))
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", k, strings.Join(r.Query[k], ",")))
		}
		fmt.Fprintf(&b, "\n      query: %s", strings.Join(parts, " "))
	}
	fmt.Fprintf(&b, "\n      body: %s", PrettyBody(r.Body))
	return b.String()
}

// PrettyBody indents a JSON body for a failure message, keeping the key order
// the handler actually sent. A body that is not JSON is quoted verbatim.
// PrettyBody, Describe, Dump and SelectorLabel are exported so a test can
// assert on the QUALITY of a failure message. A wire assertion that fails
// without showing the wire costs more time than it saves, so that rendering is
// part of this package's contract rather than an implementation detail.
func PrettyBody(body []byte) string {
	if len(body) == 0 {
		return "(empty)"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "      ", "  "); err != nil {
		return fmt.Sprintf("%q (not valid JSON)", body)
	}
	return buf.String()
}

// SelectorLabel renders a selector for a failure message.
func SelectorLabel(method, path string) string {
	switch {
	case method == "" && path == "":
		return "<any request>"
	case method == "":
		return "<any method> " + path
	case path == "":
		return strings.ToUpper(method) + " <any path>"
	default:
		return strings.ToUpper(method) + " " + path
	}
}

// ---------------------------------------------------------------------------
// Assertions on one captured request
// ---------------------------------------------------------------------------

// JSON decodes the captured body as a JSON object. It fatals when the body is
// empty or is not an object, because every field assertion built on it would
// otherwise report a misleading "field missing".
func (r Request) JSON(t *testing.T) map[string]any {
	t.Helper()
	if len(r.Body) == 0 {
		t.Fatalf("request body is empty, expected a JSON object.\n%s", r.Describe())
		return nil
	}
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("request body is not a JSON object: %v.\n%s", err, r.Describe())
		return nil
	}
	return body
}

// Field walks a dotted path such as "config.dns.blocking.active" through the
// JSON body. The second return value separates a key that was never sent from a
// key sent with a null value, which are different bugs.
func (r Request) Field(t *testing.T, dotted string) (any, bool) {
	t.Helper()
	return LookupField(r.JSON(t), dotted)
}

// AssertField fails the test unless the dotted field was sent with the wanted
// value. Numbers compare numerically, so an untyped int literal matches the
// float64 that encoding/json produces.
func (r Request) AssertField(t *testing.T, dotted string, want any) {
	t.Helper()
	got, ok := LookupField(r.JSON(t), dotted)
	if !ok {
		t.Errorf("body has no field %q, expected %#v.\n%s", dotted, want, r.Describe())
		return
	}
	if !JSONEqual(got, want) {
		t.Errorf("body field %q is %#v (%T), want %#v (%T).\n%s",
			dotted, got, got, want, want, r.Describe())
	}
}

// AssertNoField fails the test if the dotted field was sent at all, including
// when it was sent as null. Sending null and not sending are different requests
// and Pi-hole treats them differently.
func (r Request) AssertNoField(t *testing.T, dotted string) {
	t.Helper()
	got, ok := LookupField(r.JSON(t), dotted)
	if !ok {
		return
	}
	if got == nil {
		t.Errorf("body field %q was sent as null, expected it to be absent entirely.\n%s",
			dotted, r.Describe())
		return
	}
	t.Errorf("body field %q was sent as %#v, expected it to be absent.\n%s",
		dotted, got, r.Describe())
}

// AssertBodyKeys fails the test unless the body's top-level keys are exactly
// the wanted set, so an extra key the API would reject is caught as well as a
// missing one.
func (r Request) AssertBodyKeys(t *testing.T, want ...string) {
	t.Helper()
	got := slices.Sorted(maps.Keys(r.JSON(t)))
	wantSorted := slices.Sorted(slices.Values(want))
	if !slices.Equal(got, wantSorted) {
		t.Errorf("body top-level keys are %v, want exactly %v.\n%s", got, wantSorted, r.Describe())
	}
}

// AssertRawBody fails the test unless the body went onto the wire byte for
// byte as wanted. Use it for a body that is not a JSON object, such as the
// pre-encoded array the batch-delete endpoints send: if rawJSON ever stopped
// implementing json.Marshaler the array would go out as a quoted string, which
// AssertBodyKeys could not see because it is not an object at all.
func (r Request) AssertRawBody(t *testing.T, want string) {
	t.Helper()
	if got := string(r.Body); got != want {
		t.Errorf("request body is %s, want %s.\n%s", got, want, r.Describe())
	}
}

// AssertNoBody fails the test if the request carried a body, and also checks
// that no Content-Type was set. A DELETE goes through pihole.Client.Delete,
// which passes a nil body and therefore sets no Content-Type, so a body
// appearing here means the handler had started sending the identifier twice.
func (r Request) AssertNoBody(t *testing.T) {
	t.Helper()
	if len(r.Body) != 0 {
		t.Errorf("request carried a body %s, want none.\n%s", r.Body, r.Describe())
	}
	r.AssertHeader(t, "Content-Type", "")
}

// AssertQuery fails the test unless the query parameter was sent with the
// wanted value.
func (r Request) AssertQuery(t *testing.T, key, want string) {
	t.Helper()
	if !r.Query.Has(key) {
		t.Errorf("request has no query parameter %q, expected %q.\n%s", key, want, r.Describe())
		return
	}
	if got := r.Query.Get(key); got != want {
		t.Errorf("query parameter %q is %q, want %q.\n%s", key, got, want, r.Describe())
	}
}

// AssertNoQuery fails the test if the query parameter was sent at all, even
// with an empty value.
func (r Request) AssertNoQuery(t *testing.T, key string) {
	t.Helper()
	if r.Query.Has(key) {
		t.Errorf("request sent query parameter %q=%q, expected it to be absent.\n%s",
			key, r.Query.Get(key), r.Describe())
	}
}

// AssertNoQueryString fails the test if the request carried a query string at
// all, where AssertNoQuery rules out one named parameter. Most Pi-hole write
// endpoints key everything through the path and the body; /lists is the
// exception, and a parameter copied across from it would be invisible in the
// reply text.
func (r Request) AssertNoQueryString(t *testing.T) {
	t.Helper()
	if r.RawQuery != "" {
		t.Errorf("request carried a query string %q, want none.\n%s", r.RawQuery, r.Describe())
	}
}

// AssertQueryKeys fails the test unless the query string's parameter names are
// exactly the wanted set. It is the counterpart to AssertNoQueryString for the
// endpoints where a parameter is legitimate: /lists keys its type off the query
// and the config value tools key their restart flag there, so ruling out a
// query string entirely would be wrong, yet a stray extra parameter on either
// is just as invisible in the reply text as a stray one anywhere else.
func (r Request) AssertQueryKeys(t *testing.T, want ...string) {
	t.Helper()
	got := slices.Sorted(maps.Keys(r.Query))
	wantSorted := slices.Sorted(slices.Values(want))
	if !slices.Equal(got, wantSorted) {
		t.Errorf("query parameter names are %v, want exactly %v.\n%s", got, wantSorted, r.Describe())
	}
}

// AssertHeader fails the test unless the header was sent with the wanted value.
func (r Request) AssertHeader(t *testing.T, key, want string) {
	t.Helper()
	if got := r.Header.Get(key); got != want {
		t.Errorf("header %q is %q, want %q.\nheaders sent: %v\n%s",
			key, got, want, r.Header, r.Describe())
	}
}

// AssertRawPath fails the test unless the path went onto the wire escaped
// exactly as wanted, which is what distinguishes a properly escaped path
// segment from one concatenated in raw.
func (r Request) AssertRawPath(t *testing.T, want string) {
	t.Helper()
	if r.RawPath != want {
		t.Errorf("raw path is %q, want %q (it decodes to %q).\n%s",
			r.RawPath, want, r.Path, r.Describe())
	}
}

// LookupField walks a dotted path through a decoded JSON object. It reports
// whether the key exists, so a null value is found rather than missing.
func LookupField(body map[string]any, dotted string) (any, bool) {
	var cur any = body
	for _, segment := range strings.Split(dotted, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := obj[segment]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// JSONEqual compares a value decoded from JSON against one written in a test.
// encoding/json turns every number into a float64, so comparing with == would
// make AssertField(t, "dns.cache.size", 10001) unsatisfiable no matter what the
// handler sent. Numbers are therefore compared numerically across the int,
// uint and float families, and everything else structurally.
func JSONEqual(got, want any) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	if g, ok := asNumber(got); ok {
		w, ok := asNumber(want)
		return ok && g == w
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for k, wv := range w {
			gv, present := g[k]
			if !present || !JSONEqual(gv, wv) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !JSONEqual(g[i], w[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(got, want)
	}
}

// asNumber converts any Go numeric value to a float64 for comparison.
func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
