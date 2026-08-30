// Package piholefake provides a stateful, in-process emulator of the Pi-hole
// v6 REST API for the endpoints the MCP server reads and writes. It lets tests
// and a local simulation harness exercise the full multi-instance and sync
// flows — including create/update/delete round-trips — without Docker or a real
// Pi-hole.
//
// The emulator is intentionally small: it covers authentication, the gravity
// CRUD surfaces (domains, lists, groups, clients), local DNS records
// (dns.hosts and dns.cnameRecords), a stats summary, and teleporter export and
// import. It is not a faithful reimplementation of FTL; it returns the response
// shapes the client decodes and holds just enough state to make reconciliation
// observable.
package piholefake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
)

// Fake is a running in-process Pi-hole emulator backed by an httptest server.
type Fake struct {
	srv *httptest.Server

	mu           sync.Mutex
	domains      map[string]pihole.Domain
	lists        map[string]pihole.List
	groups       map[string]pihole.Group
	clients      map[string]pihole.ClientEntry
	suggestions  []pihole.ClientSuggestion
	sessions     []pihole.Session
	hosts        []string
	cnames       []string
	summaryTotal int
	nextID       int
}

// New starts a new fake Pi-hole and returns it. Call Close when done (or use
// t.Cleanup). The emulator listens on a loopback address; its base URL is
// available via URL.
func New() *Fake {
	f := &Fake{
		domains:      make(map[string]pihole.Domain),
		lists:        make(map[string]pihole.List),
		groups:       make(map[string]pihole.Group),
		clients:      make(map[string]pihole.ClientEntry),
		summaryTotal: 0,
		nextID:       1,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

// URL returns the base URL of the fake (no trailing slash, no /api suffix).
func (f *Fake) URL() string { return f.srv.URL }

// Handler returns the emulator's HTTP handler, so a caller can wrap it (in a
// request recorder, for instance) and serve it from a test server of its own.
// The Fake's built-in server stays available and independent.
func (f *Fake) Handler() http.Handler { return http.HandlerFunc(f.handle) }

// Close shuts the fake down.
func (f *Fake) Close() { f.srv.Close() }

func (f *Fake) id() int {
	id := f.nextID
	f.nextID++
	return id
}

func (f *Fake) handle(w http.ResponseWriter, r *http.Request) {
	segs := segments(r)
	if len(segs) == 0 {
		http.NotFound(w, r)
		return
	}

	// segs[0] == "api" for every Pi-hole route.
	if segs[0] != "api" {
		http.NotFound(w, r)
		return
	}
	rest := segs[1:]
	if len(rest) == 0 {
		http.NotFound(w, r)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// FTL routes the batch deletes as one literal path segment with the colon
	// inside it (/api/domains:batchDelete), not as a nested path, so an exact
	// match on "domains" never sees them. Splitting the suffix off here is what
	// keeps the four *_batch_delete tools routable against this fake.
	if category, ok := strings.CutSuffix(rest[0], ":batchDelete"); ok {
		f.handleBatchDelete(w, r, category)
		return
	}

	switch rest[0] {
	case "auth":
		f.handleAuth(w, r, itemSegments(r, "auth", 1))
	case "domains":
		// type and kind are fixed segments; the domain is the remainder.
		f.handleDomains(w, r, itemSegments(r, "domains", 2))
	case "lists":
		f.handleLists(w, r, itemSegments(r, "lists", 0))
	case "groups":
		f.handleGroups(w, r, itemSegments(r, "groups", 0))
	case "clients":
		f.handleClients(w, r, itemSegments(r, "clients", 0))
	case "config":
		// The element is a path of its own (dns/upstreams); the value that
		// follows it is the remainder, so a value carrying a slash survives.
		f.handleConfig(w, r, itemSegments(r, "config", 2))
	case "stats":
		f.handleStats(w, r, rest[1:])
	case "teleporter":
		f.handleTeleporter(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleAuth serves the login endpoint and the session listing. It used to
// ignore every path segment after /api/auth, so GET /auth/sessions came back as
// {"session":{...}} and decoded into pihole.SessionsResponse as zero sessions
// with no error at all: a tool reading the wrong endpoint looked like a Pi-hole
// with no sessions. Anything not emulated now 404s so it fails loudly instead.
func (f *Fake) handleAuth(w http.ResponseWriter, r *http.Request, p []string) {
	switch {
	case len(p) == 0:
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session": map[string]any{"valid": true, "sid": "fake-sid", "validity": 1800},
		})
	case len(p) == 1 && p[0] == "sessions" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, pihole.SessionsResponse{Sessions: f.sessions})
	case len(p) == 2 && p[0] == "session" && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) handleDomains(w http.ResponseWriter, r *http.Request, p []string) {
	switch r.Method {
	case http.MethodGet:
		// Optional /domains/{type}/{kind} filter.
		var wantType, wantKind string
		if len(p) >= 1 {
			wantType = p[0]
		}
		if len(p) >= 2 {
			wantKind = p[1]
		}
		out := make([]pihole.Domain, 0, len(f.domains))
		for _, d := range f.domains {
			if wantType != "" && d.Type != wantType {
				continue
			}
			if wantKind != "" && d.Kind != wantKind {
				continue
			}
			out = append(out, d)
		}
		writeJSON(w, http.StatusOK, pihole.DomainsResponse{Domains: out})
	case http.MethodPost:
		if len(p) < 2 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "type and kind required")
			return
		}
		body := decodeBody(r)
		// FTL types the identifier as oneOf string or array of strings
		// (domain_maybe_array), so one POST may create several rules at once.
		// Accepting only a bare string silently created a rule named "" from an
		// array payload, which no caller could see.
		names := identifiers(body["domain"])
		if len(names) == 0 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "no domain in payload")
			return
		}
		created := make([]pihole.Domain, 0, len(names))
		for _, name := range names {
			d := pihole.Domain{
				Domain:  name,
				Type:    p[0],
				Kind:    p[1],
				Comment: str(body["comment"]),
				Enabled: boolOr(body["enabled"], true),
				ID:      f.id(),
			}
			f.domains[domainKey(d.Type, d.Kind, d.Domain)] = d
			created = append(created, d)
		}
		writeJSON(w, http.StatusCreated, pihole.DomainsResponse{Domains: created})
	case http.MethodPut:
		if len(p) < 3 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "type, kind and domain required")
			return
		}
		key := domainKey(p[0], p[1], p[2])
		d, exists := f.domains[key]
		body := decodeBody(r)
		// PUT is a full replacement in FTL, not a merge. The spec's own note on
		// this endpoint reads "Ensure to send all the required parameters (such
		// as comment) to ensure these properties are retained", so every field
		// the caller omits falls back to its schema default: enabled defaults to
		// true and comment defaults to null. Preserving the stored values here
		// would hide the real defect, which is that a comment-only update
		// silently re-enables a disabled rule and an enabled-only update
		// silently wipes the rule's comment.
		d.Domain, d.Type, d.Kind = p[2], p[0], p[1]
		d.Comment = str(body["comment"])
		d.Enabled = boolOr(body["enabled"], true)
		if !exists {
			// PUT is an upsert: FTL answers 200 "Created or Updated domain" and
			// documents no 404 on this verb, unlike DELETE.
			d.ID = f.id()
		}
		f.domains[key] = d
		writeJSON(w, http.StatusOK, pihole.DomainsResponse{Domains: []pihole.Domain{d}})
	case http.MethodDelete:
		if len(p) < 3 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "type, kind and domain required")
			return
		}
		deleteOrNotFound(w, f.domains, domainKey(p[0], p[1], p[2]))
	default:
		http.NotFound(w, r)
	}
}

// listTypeValid reports whether the ?type= parameter satisfies FTL. Every
// non-GET /lists request must carry it and it must be allow or block; FTL
// answers 400 otherwise (src/api/list.c:956-968). The fake used to store
// type:"" for a missing parameter and type:"banana" for a bogus one and answer
// 201/200, so a handler that dropped the parameter round-tripped cleanly here
// and 400ed against every real Pi-hole.
func listTypeValid(typ string) bool {
	return typ == "allow" || typ == "block"
}

func (f *Fake) handleLists(w http.ResponseWriter, r *http.Request, p []string) {
	if r.Method != http.MethodGet && !listTypeValid(r.URL.Query().Get("type")) {
		writeAPIError(w, http.StatusBadRequest, "bad_request",
			`Invalid request: Specify type parameter (should be either "allow" or "block")`)
		return
	}
	switch r.Method {
	case http.MethodGet:
		wantType := r.URL.Query().Get("type")
		// Optional /lists/{address} filter, as used by the pihole://lists/{address}
		// resource.
		var wantAddress string
		if len(p) >= 1 {
			wantAddress = p[0]
		}
		out := make([]pihole.List, 0, len(f.lists))
		for _, l := range f.lists {
			if wantType != "" && l.Type != wantType {
				continue
			}
			if wantAddress != "" && l.Address != wantAddress {
				continue
			}
			out = append(out, l)
		}
		writeJSON(w, http.StatusOK, pihole.ListsResponse{Lists: out})
	case http.MethodPost:
		body := decodeBody(r)
		typ := r.URL.Query().Get("type")
		// address is oneOf string or array of strings (address_maybe_array), so
		// one POST may subscribe to several lists at once.
		addresses := identifiers(body["address"])
		if len(addresses) == 0 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "no address in payload")
			return
		}
		created := make([]pihole.List, 0, len(addresses))
		for _, address := range addresses {
			l := pihole.List{
				Address: address,
				Type:    typ,
				Comment: str(body["comment"]),
				Enabled: boolOr(body["enabled"], true),
				ID:      f.id(),
			}
			f.lists[listKey(l.Type, l.Address)] = l
			created = append(created, l)
		}
		writeJSON(w, http.StatusCreated, pihole.ListsResponse{Lists: created})
	case http.MethodPut:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "address required")
			return
		}
		typ := r.URL.Query().Get("type")
		key := listKey(typ, p[0])
		l, exists := f.lists[key]
		body := decodeBody(r)
		// Full replacement, exactly as for domains: an omitted enabled defaults
		// to true and an omitted comment is cleared. Merging into the stored
		// entry would make an enabled-only or comment-only update look lossless
		// here while it quietly resets the other field on a real Pi-hole.
		l.Address, l.Type = p[0], typ
		l.Comment = str(body["comment"])
		l.Enabled = boolOr(body["enabled"], true)
		if !exists {
			// Upsert: FTL answers 200 "Created or Updated Item" and documents no
			// 404 on PUT.
			l.ID = f.id()
		}
		f.lists[key] = l
		writeJSON(w, http.StatusOK, pihole.ListsResponse{Lists: []pihole.List{l}})
	case http.MethodDelete:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "address required")
			return
		}
		deleteOrNotFound(w, f.lists, listKey(r.URL.Query().Get("type"), p[0]))
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) handleGroups(w http.ResponseWriter, r *http.Request, p []string) {
	switch r.Method {
	case http.MethodGet:
		out := make([]pihole.Group, 0, len(f.groups))
		for _, g := range f.groups {
			out = append(out, g)
		}
		writeJSON(w, http.StatusOK, pihole.GroupsResponse{Groups: out})
	case http.MethodPost:
		body := decodeBody(r)
		// name is oneOf string or array of strings (name_maybe_array), so one
		// POST may create several groups at once.
		names := identifiers(body["name"])
		if len(names) == 0 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "no name in payload")
			return
		}
		created := make([]pihole.Group, 0, len(names))
		for _, name := range names {
			g := pihole.Group{
				Name:    name,
				Comment: str(body["comment"]),
				Enabled: boolOr(body["enabled"], true),
				ID:      f.id(),
			}
			f.groups[g.Name] = g
			created = append(created, g)
		}
		writeJSON(w, http.StatusCreated, pihole.GroupsResponse{Groups: created})
	case http.MethodPut:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "name required")
			return
		}
		g, exists := f.groups[p[0]]
		body := decodeBody(r)
		// Full replacement, as for domains and lists: an omitted enabled
		// defaults to true and an omitted comment is cleared. A group also
		// renames through this verb, since FTL's PUT schema carries name for
		// exactly that ("By specifying a different name, the group with the
		// former name as specified in the URI will be renamed"), which is what
		// pihole_groups_update's new_name argument relies on.
		g.Name = p[0]
		if newName := str(body["name"]); newName != "" {
			g.Name = newName
		}
		g.Comment = str(body["comment"])
		g.Enabled = boolOr(body["enabled"], true)
		if !exists {
			// Upsert: FTL answers 200 "Created or Updated Item" and documents no
			// 404 on PUT.
			g.ID = f.id()
		}
		if g.Name != p[0] {
			delete(f.groups, p[0])
		}
		f.groups[g.Name] = g
		writeJSON(w, http.StatusOK, pihole.GroupsResponse{Groups: []pihole.Group{g}})
	case http.MethodDelete:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "name required")
			return
		}
		deleteOrNotFound(w, f.groups, p[0])
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) handleClients(w http.ResponseWriter, r *http.Request, p []string) {
	switch r.Method {
	case http.MethodGet:
		// /clients/_suggestions is a distinct endpoint returning unconfigured
		// clients Pi-hole has seen, not a filter over the configured ones.
		// Serving the client list here decoded cleanly into the suggestions
		// envelope and produced one all-zero suggestion per configured client,
		// with no error anywhere to show for it.
		if len(p) == 1 && p[0] == "_suggestions" {
			out := append([]pihole.ClientSuggestion(nil), f.suggestions...)
			writeJSON(w, http.StatusOK, pihole.ClientSuggestionsResponse{Clients: out})
			return
		}
		// Optional /clients/{client} filter, as used by the
		// pihole://clients/{client} resource.
		var wantClient string
		if len(p) >= 1 {
			wantClient = p[0]
		}
		out := make([]pihole.ClientEntry, 0, len(f.clients))
		for _, c := range f.clients {
			if wantClient != "" && c.Client != wantClient {
				continue
			}
			out = append(out, c)
		}
		writeJSON(w, http.StatusOK, pihole.ClientsResponse{Clients: out})
	case http.MethodPost:
		body := decodeBody(r)
		// client is oneOf string or array of strings (client_maybe_array), so
		// one POST may configure several clients at once.
		names := identifiers(body["client"])
		if len(names) == 0 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "no client in payload")
			return
		}
		created := make([]pihole.ClientEntry, 0, len(names))
		for _, name := range names {
			c := pihole.ClientEntry{
				Client:  name,
				Comment: str(body["comment"]),
				ID:      f.id(),
			}
			f.clients[c.Client] = c
			created = append(created, c)
		}
		writeJSON(w, http.StatusCreated, pihole.ClientsResponse{Clients: created})
	case http.MethodPut:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "client required")
			return
		}
		c, exists := f.clients[p[0]]
		body := decodeBody(r)
		// Full replacement: an omitted comment is cleared, not kept. A client
		// carries no enabled field, so comment is the whole of it here.
		c.Client = p[0]
		c.Comment = str(body["comment"])
		if !exists {
			// Upsert: FTL answers 200 "Created or Updated Item" and documents no
			// 404 on PUT.
			c.ID = f.id()
		}
		f.clients[p[0]] = c
		writeJSON(w, http.StatusOK, pihole.ClientsResponse{Clients: []pihole.ClientEntry{c}})
	case http.MethodDelete:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "client required")
			return
		}
		deleteOrNotFound(w, f.clients, p[0])
	default:
		http.NotFound(w, r)
	}
}

// handleBatchDelete serves POST /{category}:batchDelete. The payload is an
// array of objects keyed by item, plus type (lists) or type and kind (domains).
// FTL also documents a 404 for an item that is not there, but not what happens
// to the rest of a partially matching batch, so the fake deletes what it can
// find and reports success rather than guessing at semantics it cannot verify.
func (f *Fake) handleBatchDelete(w http.ResponseWriter, r *http.Request, category string) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	switch category {
	case "domains", "lists", "groups", "clients":
	default:
		http.NotFound(w, r)
		return
	}
	for _, it := range decodeArrayBody(r) {
		item := str(it["item"])
		switch category {
		case "domains":
			delete(f.domains, domainKey(str(it["type"]), str(it["kind"]), item))
		case "lists":
			delete(f.lists, listKey(str(it["type"]), item))
		case "groups":
			delete(f.groups, item)
		case "clients":
			delete(f.clients, item)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *Fake) handleConfig(w http.ResponseWriter, r *http.Request, p []string) {
	// Only the dns section is emulated (hosts and cnameRecords). Anything else
	// 404s rather than returning an empty but valid-looking envelope: a bare
	// GET /config and a GET /config/_properties used to come back as
	// {"config":{}}, which decodes cleanly and looks like a Pi-hole with no
	// settings, so a handler reading the wrong element saw no error.
	if len(p) == 0 || p[0] != "dns" {
		writeAPIError(w, http.StatusNotFound, "not_found",
			"this fake only emulates the dns config section")
		return
	}

	// GET /config/dns → return the dns section, nested under its full path
	// from the root exactly as FTL does: {"config":{"dns":{"hosts":[...]}}}.
	// Verified in FTL's get_json_config (src/api/config.c): the builder starts
	// at the root config object and walks conf_item->p creating one object per
	// path element (config.c:519), then adds the whole tree under "config"
	// (config.c:646). The requested element is only a FILTER over which items
	// are included, never a change of root.
	//
	// The fake served this flat until the escaping session, which is why five
	// sync tests were green while pihole_instance_sync read zero local DNS and
	// CNAME records from every real Pi-hole. Both sides moved together.
	if len(p) == 1 && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, pihole.ConfigResponse{Config: map[string]any{
			"dns": map[string]any{
				"hosts":        toAnySlice(f.hosts),
				"cnameRecords": toAnySlice(f.cnames),
			},
		}})
		return
	}

	// /config/dns/{field}/{value} add (PUT) or remove (DELETE).
	if len(p) < 3 {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "field and value required")
		return
	}
	field, value := p[1], p[2]
	target := f.fieldSlice(field)
	if target == nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "unsupported config array")
		return
	}
	switch r.Method {
	case http.MethodPut:
		// FTL answers 201 with no content when an array item is added.
		f.setFieldSlice(field, appendUnique(*target, value))
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		f.setFieldSlice(field, removeValue(*target, value))
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) fieldSlice(field string) *[]string {
	switch field {
	case "hosts":
		return &f.hosts
	case "cnameRecords":
		return &f.cnames
	default:
		return nil
	}
}

func (f *Fake) setFieldSlice(field string, v []string) {
	switch field {
	case "hosts":
		f.hosts = v
	case "cnameRecords":
		f.cnames = v
	}
}

func (f *Fake) handleStats(w http.ResponseWriter, r *http.Request, p []string) {
	if len(p) == 1 && p[0] == "summary" {
		writeJSON(w, http.StatusOK, pihole.StatsSummary{
			Queries: pihole.QueryStats{Total: f.summaryTotal},
			Clients: pihole.ClientStats{Active: 1, Total: 1},
			Gravity: pihole.GravityInfo{DomainsBeingBlocked: 1},
		})
		return
	}
	http.NotFound(w, r)
}

func (f *Fake) handleTeleporter(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// A minimal stand-in archive; callers only persist and size it.
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK\x03\x04fake-teleporter-archive"))
		return
	}
	if r.Method != http.MethodPost {
		// A DELETE or a PUT used to be answered with a successful import.
		// Answering an unimplemented verb with success is a silent lie: a
		// handler that used the wrong one would look correct here and fail
		// against a real Pi-hole.
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, pihole.TeleporterImportResponse{Processed: []string{"config", "gravity"}})
}

// --- Seeding and inspection helpers (for tests and the sim harness) ---

// SetSummaryTotal sets the total query count returned by /stats/summary.
func (f *Fake) SetSummaryTotal(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summaryTotal = n
}

// AddDomain seeds a domain rule.
func (f *Fake) AddDomain(typ, kind, domain, comment string, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.domains[domainKey(typ, kind, domain)] = pihole.Domain{
		Domain: domain, Type: typ, Kind: kind, Comment: comment, Enabled: enabled, ID: f.id(),
	}
}

// AddList seeds an adlist/allowlist subscription.
func (f *Fake) AddList(typ, address, comment string, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists[listKey(typ, address)] = pihole.List{
		Address: address, Type: typ, Comment: comment, Enabled: enabled, ID: f.id(),
	}
}

// AddGroup seeds a group.
func (f *Fake) AddGroup(name, comment string, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups[name] = pihole.Group{Name: name, Comment: comment, Enabled: enabled, ID: f.id()}
}

// AddClient seeds a configured client.
func (f *Fake) AddClient(client, comment string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[client] = pihole.ClientEntry{Client: client, Comment: comment, ID: f.id()}
}

// AddClientSuggestion seeds an unconfigured client for /clients/_suggestions.
// Nothing is suggested by default: the fake has no notion of observed traffic,
// so a suggestion only exists once a test asks for one.
func (f *Fake) AddClientSuggestion(hwaddr, macVendor, addresses, names string, lastQuery int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suggestions = append(f.suggestions, pihole.ClientSuggestion{
		HWAddr:    &hwaddr,
		MacVendor: &macVendor,
		LastQuery: lastQuery,
		Addresses: &addresses,
		Names:     &names,
	})
}

// AddSession seeds an API session for /auth/sessions. Nothing is listed by
// default: the fake tracks no real sessions, so one exists only once a test
// asks for it.
func (f *Fake) AddSession(id int, remoteAddr, userAgent string, current bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, pihole.Session{
		ID:             id,
		RemoteAddr:     remoteAddr,
		UserAgent:      userAgent,
		CurrentSession: current,
	})
}

// SetHosts seeds the dns.hosts local DNS records.
func (f *Fake) SetHosts(hosts ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts = append([]string(nil), hosts...)
}

// SetCNAMEs seeds the dns.cnameRecords entries.
func (f *Fake) SetCNAMEs(cnames ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cnames = append([]string(nil), cnames...)
}

// DomainCount reports how many domain rules are currently stored.
func (f *Fake) DomainCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.domains)
}

// ListCount reports how many list subscriptions are currently stored.
func (f *Fake) ListCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.lists)
}

// GroupCount reports how many groups are currently stored.
func (f *Fake) GroupCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.groups)
}

// Hosts returns the current dns.hosts entries.
func (f *Fake) Hosts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hosts...)
}

// --- internal helpers ---

// deleteOrNotFound removes one entry and answers the way FTL does: 204 when
// something was deleted, 404 when nothing matched
// (src/api/list.c:830, JSON_SEND_OBJECT_CODE(json, deleted > 0u ? 204 : 404)).
// Answering 204 unconditionally let a tool that addressed the wrong key report
// a successful delete having removed nothing.
func deleteOrNotFound[T any](w http.ResponseWriter, m map[string]T, key string) {
	if _, ok := m[key]; !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}
	delete(m, key)
	w.WriteHeader(http.StatusNoContent)
}

func domainKey(typ, kind, domain string) string { return typ + "|" + kind + "|" + domain }
func listKey(typ, address string) string        { return typ + "|" + address }

// itemSegments splits the path after /api/<category> into a fixed number of
// leading segments plus, as a single trailing element, the WHOLE remainder.
//
// This mirrors FTL's routing rather than ordinary path splitting. Its
// startsWith (src/webserver/http-common.c:440-452) returns everything after
// the matched prefix and the following slash as one string, so FTL takes a
// list address or a client identifier whole, slashes included. Splitting the
// remainder on "/" instead, as this fake used to, made
// PUT /api/lists/https://example.com/ads.txt create a new row literally named
// "https:" and leave the real one untouched.
//
// Escaped and unescaped spellings reach the same row, and that is FTL's real
// behaviour, not a shortcut. Verified against Pi-hole v6 (pihole/pihole
// 2026.07.2) on 30 August 2026: PUT /api/lists/https://x/ads.txt and
// PUT /api/lists/https%3A%2F%2Fx%2Fads.txt both returned 200 and both updated
// the same row (id 4), leaving exactly one list stored.
//
// ONE CHARACTER BREAKS THE SYMMETRY, and this fake cannot show it. Against the
// same live instance, DELETE of the regex ^ads[0-9]+\.survey\.com returned 404
// with a bare '+' and 204 with '%2B'. Go's url.PathEscape does not escape '+',
// so escaping alone is not enough for a value that contains one. This fake
// decodes with url.PathUnescape, which accepts both spellings, so a test here
// will go green either way.
//
// CONSEQUENCE FOR ANY ESCAPING WORK: verify it against a live Pi-hole, never
// against this file.
func itemSegments(r *http.Request, category string, fixed int) []string {
	raw := strings.Trim(r.URL.EscapedPath(), "/")
	rest := strings.TrimPrefix(raw, "api/"+category)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return nil
	}
	out := make([]string, 0, fixed+1)
	for range fixed {
		seg, remainder, found := strings.Cut(rest, "/")
		out = append(out, decodeSegment(seg))
		if !found {
			return out
		}
		rest = remainder
	}
	return append(out, decodeSegment(rest))
}

// decodeSegment percent-decodes once, leaving the value alone if it is not
// valid escaping, which matches what a live Pi-hole accepts on these routes.
func decodeSegment(s string) string {
	if dec, err := url.PathUnescape(s); err == nil {
		return dec
	}
	return s
}

// segments returns the unescaped path segments of the request, preserving
// escaped slashes within a single segment (e.g. a list URL). It is used only
// to pick the category; the item within a category is taken whole by
// itemSegments.
func segments(r *http.Request) []string {
	raw := strings.Trim(r.URL.EscapedPath(), "/")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if dec, err := url.PathUnescape(p); err == nil {
			out = append(out, dec)
		} else {
			out = append(out, p)
		}
	}
	return out
}

func decodeBody(r *http.Request) map[string]any {
	if r.Body == nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// decodeArrayBody decodes a top-level JSON array of objects, the payload shape
// every :batchDelete endpoint takes.
func decodeArrayBody(r *http.Request) []map[string]any {
	if r.Body == nil {
		return nil
	}
	var items []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		return nil
	}
	return items
}

// identifiers returns the values of a field FTL types as oneOf string or array
// of strings: domain, address, name and client on their respective POSTs. Empty
// strings are dropped, so a missing field yields nothing rather than one
// nameless entry.
func identifiers(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	buf, _ := json.Marshal(v)
	_, _ = w.Write(buf)
}

func writeAPIError(w http.ResponseWriter, status int, key, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"key": key, "message": message, "hint": ""},
	})
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func boolOr(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func appendUnique(in []string, v string) []string {
	for _, s := range in {
		if s == v {
			return in
		}
	}
	return append(append([]string(nil), in...), v)
}

func removeValue(in []string, v string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}
