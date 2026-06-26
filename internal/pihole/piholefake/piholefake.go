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

	switch rest[0] {
	case "auth":
		f.handleAuth(w, r)
	case "domains":
		f.handleDomains(w, r, rest[1:])
	case "lists":
		f.handleLists(w, r, rest[1:])
	case "groups":
		f.handleGroups(w, r, rest[1:])
	case "clients":
		f.handleClients(w, r, rest[1:])
	case "config":
		f.handleConfig(w, r, rest[1:])
	case "stats":
		f.handleStats(w, r, rest[1:])
	case "teleporter":
		f.handleTeleporter(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session": map[string]any{"valid": true, "sid": "fake-sid", "validity": 1800},
	})
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
		domain, _ := body["domain"].(string)
		d := pihole.Domain{
			Domain:  domain,
			Type:    p[0],
			Kind:    p[1],
			Comment: str(body["comment"]),
			Enabled: boolOr(body["enabled"], true),
			ID:      f.id(),
		}
		f.domains[domainKey(d.Type, d.Kind, d.Domain)] = d
		writeJSON(w, http.StatusCreated, pihole.DomainsResponse{Domains: []pihole.Domain{d}})
	case http.MethodPut:
		if len(p) < 3 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "type, kind and domain required")
			return
		}
		key := domainKey(p[0], p[1], p[2])
		d, ok := f.domains[key]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found", "domain not found")
			return
		}
		body := decodeBody(r)
		if v, ok := body["comment"]; ok {
			d.Comment = str(v)
		}
		d.Enabled = boolOr(body["enabled"], d.Enabled)
		f.domains[key] = d
		writeJSON(w, http.StatusOK, pihole.DomainsResponse{Domains: []pihole.Domain{d}})
	case http.MethodDelete:
		if len(p) < 3 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "type, kind and domain required")
			return
		}
		delete(f.domains, domainKey(p[0], p[1], p[2]))
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) handleLists(w http.ResponseWriter, r *http.Request, p []string) {
	switch r.Method {
	case http.MethodGet:
		wantType := r.URL.Query().Get("type")
		out := make([]pihole.List, 0, len(f.lists))
		for _, l := range f.lists {
			if wantType != "" && l.Type != wantType {
				continue
			}
			out = append(out, l)
		}
		writeJSON(w, http.StatusOK, pihole.ListsResponse{Lists: out})
	case http.MethodPost:
		body := decodeBody(r)
		typ := r.URL.Query().Get("type")
		l := pihole.List{
			Address: str(body["address"]),
			Type:    typ,
			Comment: str(body["comment"]),
			Enabled: boolOr(body["enabled"], true),
			ID:      f.id(),
		}
		f.lists[listKey(l.Type, l.Address)] = l
		writeJSON(w, http.StatusCreated, pihole.ListsResponse{Lists: []pihole.List{l}})
	case http.MethodPut:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "address required")
			return
		}
		typ := r.URL.Query().Get("type")
		key := listKey(typ, p[0])
		l, ok := f.lists[key]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found", "list not found")
			return
		}
		body := decodeBody(r)
		if v, ok := body["comment"]; ok {
			l.Comment = str(v)
		}
		l.Enabled = boolOr(body["enabled"], l.Enabled)
		f.lists[key] = l
		writeJSON(w, http.StatusOK, pihole.ListsResponse{Lists: []pihole.List{l}})
	case http.MethodDelete:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "address required")
			return
		}
		delete(f.lists, listKey(r.URL.Query().Get("type"), p[0]))
		w.WriteHeader(http.StatusNoContent)
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
		g := pihole.Group{
			Name:    str(body["name"]),
			Comment: str(body["comment"]),
			Enabled: boolOr(body["enabled"], true),
			ID:      f.id(),
		}
		f.groups[g.Name] = g
		writeJSON(w, http.StatusCreated, pihole.GroupsResponse{Groups: []pihole.Group{g}})
	case http.MethodPut:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "name required")
			return
		}
		g, ok := f.groups[p[0]]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found", "group not found")
			return
		}
		body := decodeBody(r)
		if v, ok := body["comment"]; ok {
			g.Comment = str(v)
		}
		g.Enabled = boolOr(body["enabled"], g.Enabled)
		f.groups[p[0]] = g
		writeJSON(w, http.StatusOK, pihole.GroupsResponse{Groups: []pihole.Group{g}})
	case http.MethodDelete:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "name required")
			return
		}
		delete(f.groups, p[0])
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) handleClients(w http.ResponseWriter, r *http.Request, p []string) {
	switch r.Method {
	case http.MethodGet:
		out := make([]pihole.ClientEntry, 0, len(f.clients))
		for _, c := range f.clients {
			out = append(out, c)
		}
		writeJSON(w, http.StatusOK, pihole.ClientsResponse{Clients: out})
	case http.MethodPost:
		body := decodeBody(r)
		c := pihole.ClientEntry{
			Client:  str(body["client"]),
			Comment: str(body["comment"]),
			ID:      f.id(),
		}
		f.clients[c.Client] = c
		writeJSON(w, http.StatusCreated, pihole.ClientsResponse{Clients: []pihole.ClientEntry{c}})
	case http.MethodPut:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "client required")
			return
		}
		c, ok := f.clients[p[0]]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found", "client not found")
			return
		}
		body := decodeBody(r)
		if v, ok := body["comment"]; ok {
			c.Comment = str(v)
		}
		f.clients[p[0]] = c
		writeJSON(w, http.StatusOK, pihole.ClientsResponse{Clients: []pihole.ClientEntry{c}})
	case http.MethodDelete:
		if len(p) < 1 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "client required")
			return
		}
		delete(f.clients, p[0])
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) handleConfig(w http.ResponseWriter, r *http.Request, p []string) {
	// Only the dns section is emulated (hosts and cnameRecords).
	if len(p) == 0 || p[0] != "dns" {
		writeJSON(w, http.StatusOK, pihole.ConfigResponse{Config: map[string]any{}})
		return
	}

	// GET /config/dns → return the dns section.
	if len(p) == 1 && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, pihole.ConfigResponse{Config: map[string]any{
			"hosts":        toAnySlice(f.hosts),
			"cnameRecords": toAnySlice(f.cnames),
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
		f.setFieldSlice(field, appendUnique(*target, value))
		writeJSON(w, http.StatusOK, pihole.ConfigResponse{Config: map[string]any{}})
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

func domainKey(typ, kind, domain string) string { return typ + "|" + kind + "|" + domain }
func listKey(typ, address string) string        { return typ + "|" + address }

// segments returns the unescaped path segments of the request, preserving
// escaped slashes within a single segment (e.g. a list URL).
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
