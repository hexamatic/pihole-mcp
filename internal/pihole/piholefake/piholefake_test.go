package piholefake_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
)

// These tests pin the emulator to the contract FTL actually publishes, because
// every test built on top of the fake inherits whatever the fake believes. The
// two that matter most: PUT is a full replacement (an omitted enabled defaults
// to true, an omitted comment is cleared), and the identifier on a POST may be
// an array as readily as a string.

// newFake starts a fake and returns it with a request helper bound to it.
func newFake(t *testing.T) *piholefake.Fake {
	t.Helper()
	f := piholefake.New()
	t.Cleanup(f.Close)
	return f
}

// do issues a request against the fake and returns the status and raw body.
func do(t *testing.T, f *piholefake.Fake, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, f.URL()+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, raw
}

// decodeInto unmarshals a response body, failing the test on malformed JSON.
func decodeInto(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

// getDomains reads the stored domains back, so an assertion never trusts the
// status code of the write that preceded it.
func getDomains(t *testing.T, f *piholefake.Fake) []pihole.Domain {
	t.Helper()
	status, raw := do(t, f, http.MethodGet, "/api/domains", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/domains status = %d, want 200", status)
	}
	var resp pihole.DomainsResponse
	decodeInto(t, raw, &resp)
	return resp.Domains
}

func getLists(t *testing.T, f *piholefake.Fake) []pihole.List {
	t.Helper()
	status, raw := do(t, f, http.MethodGet, "/api/lists", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/lists status = %d, want 200", status)
	}
	var resp pihole.ListsResponse
	decodeInto(t, raw, &resp)
	return resp.Lists
}

func getGroups(t *testing.T, f *piholefake.Fake) []pihole.Group {
	t.Helper()
	status, raw := do(t, f, http.MethodGet, "/api/groups", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/groups status = %d, want 200", status)
	}
	var resp pihole.GroupsResponse
	decodeInto(t, raw, &resp)
	return resp.Groups
}

func getClients(t *testing.T, f *piholefake.Fake) []pihole.ClientEntry {
	t.Helper()
	status, raw := do(t, f, http.MethodGet, "/api/clients", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/clients status = %d, want 200", status)
	}
	var resp pihole.ClientsResponse
	decodeInto(t, raw, &resp)
	return resp.Clients
}

func onlyClient(t *testing.T, f *piholefake.Fake) pihole.ClientEntry {
	t.Helper()
	got := getClients(t, f)
	if len(got) != 1 {
		t.Fatalf("clients = %d, want 1: %+v", len(got), got)
	}
	return got[0]
}

func onlyDomain(t *testing.T, f *piholefake.Fake) pihole.Domain {
	t.Helper()
	got := getDomains(t, f)
	if len(got) != 1 {
		t.Fatalf("domains = %d, want 1: %+v", len(got), got)
	}
	return got[0]
}

func onlyList(t *testing.T, f *piholefake.Fake) pihole.List {
	t.Helper()
	got := getLists(t, f)
	if len(got) != 1 {
		t.Fatalf("lists = %d, want 1: %+v", len(got), got)
	}
	return got[0]
}

func onlyGroup(t *testing.T, f *piholefake.Fake) pihole.Group {
	t.Helper()
	got := getGroups(t, f)
	if len(got) != 1 {
		t.Fatalf("groups = %d, want 1: %+v", len(got), got)
	}
	return got[0]
}

// --- PUT is a full replacement: an omitted enabled defaults to true ---

func TestPutDomainWithoutEnabledReEnables(t *testing.T) {
	f := newFake(t)
	f.AddDomain("deny", "exact", "ads.example.com", "blocked", false)

	status, _ := do(t, f, http.MethodPut, "/api/domains/deny/exact/ads.example.com",
		map[string]any{"comment": "still blocked"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyDomain(t, f)
	if !got.Enabled {
		t.Error("comment-only PUT left the domain disabled; real FTL defaults enabled to true on a replace, which is the production bug this fake must reproduce")
	}
}

func TestPutDomainEnabledFalseDisables(t *testing.T) {
	f := newFake(t)
	f.AddDomain("deny", "exact", "ads.example.com", "blocked", true)

	status, _ := do(t, f, http.MethodPut, "/api/domains/deny/exact/ads.example.com",
		map[string]any{"comment": "blocked", "enabled": false})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyDomain(t, f)
	if got.Enabled {
		t.Error("PUT with enabled=false left the domain enabled")
	}
}

func TestPutListWithoutEnabledReEnables(t *testing.T) {
	f := newFake(t)
	f.AddList("block", "https://lists.example.com/ads.txt", "ad list", false)

	status, _ := do(t, f, http.MethodPut,
		"/api/lists/https%3A%2F%2Flists.example.com%2Fads.txt?type=block",
		map[string]any{"comment": "ad list"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyList(t, f)
	if !got.Enabled {
		t.Error("comment-only PUT left the list disabled; real FTL defaults enabled to true on a replace")
	}
}

func TestPutListEnabledFalseDisables(t *testing.T) {
	f := newFake(t)
	f.AddList("block", "https://lists.example.com/ads.txt", "ad list", true)

	status, _ := do(t, f, http.MethodPut,
		"/api/lists/https%3A%2F%2Flists.example.com%2Fads.txt?type=block",
		map[string]any{"comment": "ad list", "enabled": false})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyList(t, f)
	if got.Enabled {
		t.Error("PUT with enabled=false left the list enabled")
	}
}

func TestPutGroupWithoutEnabledReEnables(t *testing.T) {
	f := newFake(t)
	f.AddGroup("iot", "smart devices", false)

	status, _ := do(t, f, http.MethodPut, "/api/groups/iot",
		map[string]any{"comment": "smart devices"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyGroup(t, f)
	if !got.Enabled {
		t.Error("comment-only PUT left the group disabled; real FTL defaults enabled to true on a replace")
	}
}

func TestPutGroupEnabledFalseDisables(t *testing.T) {
	f := newFake(t)
	f.AddGroup("iot", "smart devices", true)

	status, _ := do(t, f, http.MethodPut, "/api/groups/iot",
		map[string]any{"comment": "smart devices", "enabled": false})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyGroup(t, f)
	if got.Enabled {
		t.Error("PUT with enabled=false left the group enabled")
	}
}

// --- PUT is a full replacement: an omitted comment is cleared ---

func TestPutDomainWithoutCommentClearsIt(t *testing.T) {
	f := newFake(t)
	f.AddDomain("deny", "exact", "ads.example.com", "raised by the security team", true)

	status, _ := do(t, f, http.MethodPut, "/api/domains/deny/exact/ads.example.com",
		map[string]any{"enabled": false})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyDomain(t, f)
	if got.Comment != "" {
		t.Errorf("comment = %q, want empty: an enabled-only PUT wipes the comment on real FTL, so the fake must not merge it", got.Comment)
	}
}

// The comment-clearing correction was exercised for domains only. Reverting
// the fake's lists, groups and clients handlers to the old merge behaviour
// produced no failing test anywhere in the repo, so three of the four
// corrections were undefended. internal/tools/lists.go and clients.go both omit
// comment on an enabled-only update, which is exactly the path that needs the
// guard.

func TestPutListWithoutCommentClearsIt(t *testing.T) {
	f := newFake(t)
	f.AddList("block", "https://example.com/list.txt", "vendor supplied", true)

	status, _ := do(t, f, http.MethodPut, "/api/lists/https://example.com/list.txt?type=block",
		map[string]any{"enabled": false})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyList(t, f)
	if got.Comment != "" {
		t.Errorf("comment = %q, want empty: an enabled-only PUT wipes the comment on real FTL, so the fake must not merge it", got.Comment)
	}
}

func TestPutGroupWithoutCommentClearsIt(t *testing.T) {
	f := newFake(t)
	f.AddGroup("kids", "childrens devices", true)

	status, _ := do(t, f, http.MethodPut, "/api/groups/kids",
		map[string]any{"enabled": false})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyGroup(t, f)
	if got.Comment != "" {
		t.Errorf("comment = %q, want empty: an enabled-only PUT wipes the comment on real FTL, so the fake must not merge it", got.Comment)
	}
}

func TestPutClientWithoutCommentClearsIt(t *testing.T) {
	f := newFake(t)
	f.AddClient("192.168.1.50", "kids tablet")

	status, _ := do(t, f, http.MethodPut, "/api/clients/192.168.1.50",
		map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyClient(t, f)
	if got.Comment != "" {
		t.Errorf("comment = %q, want empty: a PUT that omits comment wipes it on real FTL, so the fake must not merge it", got.Comment)
	}
}

func TestPutGroupRenames(t *testing.T) {
	f := newFake(t)
	f.AddGroup("iot", "smart devices", true)

	status, _ := do(t, f, http.MethodPut, "/api/groups/iot",
		map[string]any{"name": "iot-devices", "comment": "smart devices"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyGroup(t, f)
	if got.Name != "iot-devices" {
		t.Errorf("group name = %q, want %q: a PUT carrying name renames the group on real FTL", got.Name, "iot-devices")
	}
}

func TestPutCreatesMissingDomain(t *testing.T) {
	f := newFake(t)

	status, _ := do(t, f, http.MethodPut, "/api/domains/deny/exact/new.example.com",
		map[string]any{"comment": "added by replace"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: FTL answers \"Created or Updated domain\" and documents no 404 on this verb", status)
	}

	got := onlyDomain(t, f)
	if got.Domain != "new.example.com" || !got.Enabled || got.Comment != "added by replace" {
		t.Errorf("upserted domain = %+v, want new.example.com enabled with the supplied comment", got)
	}
}

// The PUT-is-an-upsert correction was exercised for domains only. Restoring
// 404-on-missing to the lists, groups and clients handlers produced no failing
// test, so those three corrections could be undone unnoticed.

func TestPutCreatesMissingList(t *testing.T) {
	f := newFake(t)

	status, _ := do(t, f, http.MethodPut, "/api/lists/https://new.example.com/l.txt?type=block",
		map[string]any{"comment": "added by replace"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: FTL answers \"Created or Updated\" and documents no 404 on this verb", status)
	}

	got := onlyList(t, f)
	if got.Address != "https://new.example.com/l.txt" || !got.Enabled || got.Comment != "added by replace" {
		t.Errorf("upserted list = %+v, want the new address enabled with the supplied comment", got)
	}
}

func TestPutCreatesMissingGroup(t *testing.T) {
	f := newFake(t)

	status, _ := do(t, f, http.MethodPut, "/api/groups/newgroup",
		map[string]any{"comment": "added by replace"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyGroup(t, f)
	if got.Name != "newgroup" || !got.Enabled || got.Comment != "added by replace" {
		t.Errorf("upserted group = %+v, want newgroup enabled with the supplied comment", got)
	}
}

func TestPutCreatesMissingClient(t *testing.T) {
	f := newFake(t)

	status, _ := do(t, f, http.MethodPut, "/api/clients/192.168.1.77",
		map[string]any{"comment": "added by replace"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyClient(t, f)
	if got.Client != "192.168.1.77" || got.Comment != "added by replace" {
		t.Errorf("upserted client = %+v, want 192.168.1.77 with the supplied comment", got)
	}
}

// --- POST accepts the identifier as a string or an array of strings ---

func TestPostDomainArrayCreatesOnePerElement(t *testing.T) {
	f := newFake(t)

	status, raw := do(t, f, http.MethodPost, "/api/domains/deny/exact",
		map[string]any{"domain": []string{"one.example.com", "two.example.com"}, "comment": "batch"})
	if status != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201", status)
	}

	var resp pihole.DomainsResponse
	decodeInto(t, raw, &resp)
	if len(resp.Domains) != 2 {
		t.Errorf("response carried %d domains, want 2: the envelope must list every entry created", len(resp.Domains))
	}

	got := getDomains(t, f)
	if len(got) != 2 {
		t.Fatalf("stored domains = %d, want 2: %+v", len(got), got)
	}
	seen := map[string]bool{}
	for _, d := range got {
		seen[d.Domain] = true
		if d.Type != "deny" || d.Kind != "exact" || d.Comment != "batch" || !d.Enabled {
			t.Errorf("stored domain %+v did not inherit the shared fields", d)
		}
	}
	if !seen["one.example.com"] || !seen["two.example.com"] {
		t.Errorf("stored domains = %+v, want one.example.com and two.example.com", got)
	}
}

func TestPostDomainStringCreatesOne(t *testing.T) {
	f := newFake(t)

	status, raw := do(t, f, http.MethodPost, "/api/domains/deny/exact",
		map[string]any{"domain": "single.example.com", "comment": "solo"})
	if status != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201", status)
	}

	var resp pihole.DomainsResponse
	decodeInto(t, raw, &resp)
	if len(resp.Domains) != 1 {
		t.Fatalf("response carried %d domains, want 1", len(resp.Domains))
	}

	got := onlyDomain(t, f)
	if got.Domain != "single.example.com" || got.Comment != "solo" || !got.Enabled {
		t.Errorf("stored domain = %+v, want single.example.com enabled with comment solo", got)
	}
}

func TestPostDomainWithoutIdentifierIsRejected(t *testing.T) {
	f := newFake(t)

	status, _ := do(t, f, http.MethodPost, "/api/domains/deny/exact",
		map[string]any{"comment": "no domain here"})
	if status != http.StatusBadRequest {
		t.Errorf("POST status = %d, want 400", status)
	}
	if n := f.DomainCount(); n != 0 {
		t.Errorf("DomainCount = %d, want 0: a payload with no domain must not create a nameless rule", n)
	}
}

func TestPostListArrayCreatesOnePerElement(t *testing.T) {
	f := newFake(t)

	status, _ := do(t, f, http.MethodPost, "/api/lists?type=block",
		map[string]any{"address": []string{"https://a.example.com/l.txt", "https://b.example.com/l.txt"}})
	if status != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201", status)
	}

	if n := f.ListCount(); n != 2 {
		t.Errorf("ListCount = %d, want 2", n)
	}
}

// identifiers([]any) on the groups and clients POSTs was likewise undefended:
// both reverted to string-only with no test failing.

func TestPostGroupArrayCreatesOnePerElement(t *testing.T) {
	f := newFake(t)

	status, _ := do(t, f, http.MethodPost, "/api/groups",
		map[string]any{"name": []string{"kids", "guests"}, "comment": "batch"})
	if status != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201", status)
	}

	if n := f.GroupCount(); n != 2 {
		t.Errorf("GroupCount = %d, want 2: name is oneOf string or array, so one POST creates several", n)
	}
	for _, g := range getGroups(t, f) {
		if g.Name != "kids" && g.Name != "guests" {
			t.Errorf("unexpected group %q: the array elements must each become their own row", g.Name)
		}
	}
}

func TestPostClientArrayCreatesOnePerElement(t *testing.T) {
	f := newFake(t)

	status, _ := do(t, f, http.MethodPost, "/api/clients",
		map[string]any{"client": []string{"192.168.1.10", "192.168.1.11"}, "comment": "batch"})
	if status != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201", status)
	}

	got := getClients(t, f)
	if len(got) != 2 {
		t.Errorf("clients = %d, want 2: client is oneOf string or array, so one POST configures several", len(got))
	}
	for _, c := range got {
		if c.Client != "192.168.1.10" && c.Client != "192.168.1.11" {
			t.Errorf("unexpected client %q: the array elements must each become their own row", c.Client)
		}
	}
}

// FTL matches the item against the URL-DECODED path (startsWith reads
// local_uri_raw), so the escaped and the unescaped spelling of the same list
// address must reach the same stored row. The fake used to split the remainder
// on "/", which made the unescaped spelling create a new row called "https:"
// and leave the real one untouched, so a handler that addressed the wrong row
// would have looked correct here.
func TestListItemIsTakenWhole(t *testing.T) {
	const address = "https://example.com/ads.txt"

	f := newFake(t)
	f.AddList("block", address, "vendor supplied", true)

	// FTL's startsWith returns everything after "/api/lists/" as one string, so
	// the two slashes inside the address are part of the item and not path
	// separators. Splitting on "/" here used to create a row named "https:".
	status, _ := do(t, f, http.MethodPut, "/api/lists/"+address+"?type=block", map[string]any{"comment": "edited"})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", status)
	}

	got := onlyList(t, f)
	if got.Address != address {
		t.Errorf("address = %q, want %q: the whole remainder after /lists is one item", got.Address, address)
	}
	if got.Comment != "edited" {
		t.Errorf("comment = %q, want %q: the PUT updated a different row", got.Comment, "edited")
	}
}

// Both spellings address the same row, verified against Pi-hole v6 2026.07.2
// on 30 August 2026: a raw PUT and a %2F-escaped PUT of the same list address
// each returned 200 and updated the same stored row.
//
// The symmetry does NOT extend to '+': live FTL answered 404 for a bare '+' in
// a regex path and 204 for '%2B'. This fake accepts both, so it cannot be used
// as evidence about escaping. See the note on itemSegments.
func TestListItemEscapedAndRawAddressTheSameRow(t *testing.T) {
	const address = "https://example.com/ads.txt"

	for _, tc := range []struct{ name, path string }{
		{"raw", "/api/lists/" + address + "?type=block"},
		{"escaped", "/api/lists/" + url.PathEscape(address) + "?type=block"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.AddList("block", address, "vendor supplied", true)

			status, _ := do(t, f, http.MethodPut, tc.path, map[string]any{"comment": "edited"})
			if status != http.StatusOK {
				t.Fatalf("PUT status = %d, want 200", status)
			}
			got := onlyList(t, f)
			if got.Address != address {
				t.Errorf("address = %q, want %q: both spellings name the same row", got.Address, address)
			}
			if got.Comment != "edited" {
				t.Errorf("comment = %q, want %q: the PUT updated a different row", got.Comment, "edited")
			}
		})
	}
}

// --- An endpoint the fake does not emulate must fail loudly, not lie ---

// Answering the wrong shape with a 200 is worse than answering nothing: it
// decodes cleanly into the caller's type and looks like an empty Pi-hole.
func TestUnemulatedEndpointsAreNotAnsweredWithASuccessfulLie(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
	}{
		// GET /auth/sessions used to return {"session":{...}}, which decodes
		// into pihole.SessionsResponse as zero sessions with no error.
		{"auth unknown sub-path", http.MethodGet, "/api/auth/nonsense"},
		// A bare /config and /config/_properties used to return {"config":{}}.
		{"config properties", http.MethodGet, "/api/config/_properties"},
		{"config unknown section", http.MethodGet, "/api/config/webserver"},
		// The teleporter used to answer ANY verb with a successful import.
		{"teleporter delete", http.MethodDelete, "/api/teleporter"},
		{"teleporter put", http.MethodPut, "/api/teleporter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			status, _ := do(t, f, tc.method, tc.path, nil)
			if status != http.StatusNotFound {
				t.Errorf("%s %s status = %d, want 404: an unemulated endpoint must fail loudly rather than decode as empty-but-valid",
					tc.method, tc.path, status)
			}
		})
	}
}

// The session listing is emulated, so it must answer with the real shape.
func TestAuthSessionsServesTheSessionListing(t *testing.T) {
	f := newFake(t)
	f.AddSession(5, "192.168.1.10", "curl/8", true)

	status, raw := do(t, f, http.MethodGet, "/api/auth/sessions", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/auth/sessions status = %d, want 200", status)
	}
	var resp pihole.SessionsResponse
	decodeInto(t, raw, &resp)
	if len(resp.Sessions) != 1 || resp.Sessions[0].ID != 5 {
		t.Errorf("sessions = %+v, want the one seeded session with id 5", resp.Sessions)
	}
}

// --- /lists requires a valid type on every non-GET (list.c:956-968) ---

func TestListWriteRequiresAValidType(t *testing.T) {
	for _, tc := range []struct{ name, path, method string }{
		{"post without type", "/api/lists", http.MethodPost},
		{"post with a bogus type", "/api/lists?type=banana", http.MethodPost},
		{"put without type", "/api/lists/https://example.com/l.txt", http.MethodPut},
		{"delete without type", "/api/lists/https://example.com/l.txt", http.MethodDelete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			var body any
			if tc.method != http.MethodDelete {
				body = map[string]any{"address": "https://example.com/l.txt"}
			}
			status, _ := do(t, f, tc.method, tc.path, body)
			if status != http.StatusBadRequest {
				t.Errorf("%s %s status = %d, want 400: FTL requires type to be allow or block on every non-GET /lists request",
					tc.method, tc.path, status)
			}
			if n := f.ListCount(); n != 0 {
				t.Errorf("ListCount = %d, want 0: a rejected request must not store anything", n)
			}
		})
	}
}

// A GET does not need the type: it is the one verb where FTL treats it as
// optional, and the tools rely on that to list every subscription at once.
func TestListGetDoesNotRequireAType(t *testing.T) {
	f := newFake(t)
	f.AddList("block", "https://example.com/l.txt", "", true)

	status, _ := do(t, f, http.MethodGet, "/api/lists", nil)
	if status != http.StatusOK {
		t.Errorf("GET /api/lists status = %d, want 200: type is optional on a GET", status)
	}
}

// --- Deleting nothing is a 404, not a 204 (list.c:830) ---

func TestDeleteOfAMissingItemIs404(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{"domain", "/api/domains/deny/exact/absent.example.com"},
		{"list", "/api/lists/https://absent.example.com/l.txt?type=block"},
		{"group", "/api/groups/absent"},
		{"client", "/api/clients/10.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			status, _ := do(t, f, http.MethodDelete, tc.path, nil)
			if status != http.StatusNotFound {
				t.Errorf("DELETE %s status = %d, want 404: FTL answers 204 only when something was actually deleted, so a 204 here would let a tool that addressed the wrong key report success",
					tc.path, status)
			}
		})
	}
}

func TestDeleteOfAPresentItemIs204(t *testing.T) {
	f := newFake(t)
	f.AddDomain("deny", "exact", "present.example.com", "", true)

	status, _ := do(t, f, http.MethodDelete, "/api/domains/deny/exact/present.example.com", nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", status)
	}
	if n := f.DomainCount(); n != 0 {
		t.Errorf("DomainCount = %d, want 0", n)
	}
}

// --- Batch deletes are a literal path segment, not a nested path ---

func TestBatchDeleteDomains(t *testing.T) {
	f := newFake(t)
	f.AddDomain("deny", "exact", "one.example.com", "", true)
	f.AddDomain("deny", "exact", "two.example.com", "", true)
	f.AddDomain("allow", "exact", "keep.example.com", "", true)

	status, _ := do(t, f, http.MethodPost, "/api/domains:batchDelete", []map[string]any{
		{"item": "one.example.com", "type": "deny", "kind": "exact"},
		{"item": "two.example.com", "type": "deny", "kind": "exact"},
	})
	if status != http.StatusNoContent {
		t.Fatalf("POST /api/domains:batchDelete status = %d, want 204", status)
	}

	got := getDomains(t, f)
	if len(got) != 1 || got[0].Domain != "keep.example.com" {
		t.Errorf("remaining domains = %+v, want only keep.example.com", got)
	}
}

func TestBatchDeleteGroups(t *testing.T) {
	f := newFake(t)
	f.AddGroup("iot", "", true)
	f.AddGroup("guest", "", true)

	status, _ := do(t, f, http.MethodPost, "/api/groups:batchDelete", []map[string]any{
		{"item": "iot"},
	})
	if status != http.StatusNoContent {
		t.Fatalf("POST /api/groups:batchDelete status = %d, want 204", status)
	}

	got := onlyGroup(t, f)
	if got.Name != "guest" {
		t.Errorf("remaining group = %q, want guest", got.Name)
	}
}

// --- Client suggestions are their own endpoint, not the configured list ---

func TestClientSuggestionsAreNotTheClientList(t *testing.T) {
	f := newFake(t)
	f.AddClient("192.168.1.50", "laptop")
	f.AddClientSuggestion("12:34:56:78:9a:bc", "Espressif Inc.", "192.168.1.77", "sensor", 1683305917)

	status, raw := do(t, f, http.MethodGet, "/api/clients/_suggestions", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/clients/_suggestions status = %d, want 200", status)
	}

	var resp pihole.ClientSuggestionsResponse
	decodeInto(t, raw, &resp)
	if len(resp.Clients) != 1 {
		t.Fatalf("suggestions = %d, want 1 (the configured client must not be suggested): %+v", len(resp.Clients), resp.Clients)
	}
	if resp.Clients[0].HWAddr == nil || *resp.Clients[0].HWAddr != "12:34:56:78:9a:bc" {
		t.Errorf("suggestion hwaddr = %v, want 12:34:56:78:9a:bc: serving the client list here decodes into all-zero suggestions", resp.Clients[0].HWAddr)
	}
}

func TestGetSingleClientFilters(t *testing.T) {
	f := newFake(t)
	f.AddClient("192.168.1.50", "laptop")
	f.AddClient("192.168.1.51", "printer")

	status, raw := do(t, f, http.MethodGet, "/api/clients/192.168.1.51", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/clients/{client} status = %d, want 200", status)
	}

	var resp pihole.ClientsResponse
	decodeInto(t, raw, &resp)
	if len(resp.Clients) != 1 || resp.Clients[0].Client != "192.168.1.51" {
		t.Errorf("clients = %+v, want only 192.168.1.51", resp.Clients)
	}
}

func TestGetSingleListFilters(t *testing.T) {
	f := newFake(t)
	f.AddList("block", "https://a.example.com/l.txt", "first", true)
	f.AddList("block", "https://b.example.com/l.txt", "second", true)

	status, raw := do(t, f, http.MethodGet,
		"/api/lists/https%3A%2F%2Fb.example.com%2Fl.txt?type=block", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/lists/{address} status = %d, want 200", status)
	}

	var resp pihole.ListsResponse
	decodeInto(t, raw, &resp)
	if len(resp.Lists) != 1 || resp.Lists[0].Address != "https://b.example.com/l.txt" {
		t.Errorf("lists = %+v, want only https://b.example.com/l.txt", resp.Lists)
	}
}

// --- Config array add ---

func TestConfigArrayAddReturns201(t *testing.T) {
	f := newFake(t)

	status, raw := do(t, f, http.MethodPut, "/api/config/dns/hosts/192.168.1.2%20pi.hole", nil)
	if status != http.StatusCreated {
		t.Errorf("PUT config array item status = %d, want 201", status)
	}
	if len(raw) != 0 {
		t.Errorf("body = %q, want empty: FTL sends no content on success", raw)
	}

	got := f.Hosts()
	if len(got) != 1 || got[0] != "192.168.1.2 pi.hole" {
		t.Errorf("hosts = %+v, want the added entry", got)
	}
}
