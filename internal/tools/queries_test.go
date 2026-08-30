package tools

import (
	"strings"
	"testing"
)

// queriesFilterArgs are the twelve filter arguments pihole_queries_search
// accepts. Every one of them travels only in the query string, so the rendered
// output cannot show whether it was sent, sent under the wrong name, or
// dropped.
var queriesFilterArgs = []string{
	"domain", "client_ip", "client_name", "upstream", "type", "status",
	"reply", "dnssec", "from", "until", "length", "cursor",
}

func TestQueriesSearch_Normal(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries": []any{
				map[string]any{
					"id": 1, "time": 1700000000.0, "type": "A", "domain": "example.com",
					"cname": nil, "status": "FORWARDED",
					"client":   map[string]any{"ip": "192.168.1.10", "name": "desktop"},
					"dnssec":   "INSECURE",
					"reply":    map[string]any{"type": "IP", "time": 5.2},
					"list_id":  nil,
					"upstream": "1.1.1.1#53",
				},
			},
			"cursor":          100,
			"recordsTotal":    50000,
			"recordsFiltered": 150,
		},
	})
	c := newTestClient(t, h)

	text := callTool(t, queriesSearchHandler, c, nil)
	if !strings.Contains(text, "1 of 150 queries") {
		t.Errorf("expected '1 of 150 queries', got: %s", text)
	}
	if !strings.Contains(text, "example.com") {
		t.Errorf("expected domain in output, got: %s", text)
	}
	if !strings.Contains(text, "FORWARDED") {
		t.Errorf("expected status in output, got: %s", text)
	}
	if !strings.Contains(text, "cursor=100") {
		t.Errorf("expected cursor pagination, got: %s", text)
	}

	// An unsupplied filter must be left out of the query string entirely. FTL
	// reads "?domain=" as a request for the empty domain rather than for
	// everything, so an argument that leaked through as an empty value would
	// hand the model an empty result set it would read as "no such traffic".
	req := h.Only(t, "GET", "/queries")
	req.AssertQuery(t, "length", "25")
	for _, arg := range queriesFilterArgs {
		if arg == "length" {
			continue
		}
		req.AssertNoQuery(t, arg)
	}
}

// TestQueriesSearch_ForwardsEveryFilter pins the wire name of all twelve filter
// arguments at once. The handler renames several of them on the way through
// (max_results is not the API's name, and the MCP argument names are not
// guaranteed to match FTL's), and a filter that silently fails to apply is the
// worst outcome of the lot: the model gets a full unfiltered query log and
// reasons about it as though it were the narrow slice it asked for.
func TestQueriesSearch_ForwardsEveryFilter(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries":         []any{},
			"cursor":          0,
			"recordsTotal":    0,
			"recordsFiltered": 0,
		},
	})
	c := newTestClient(t, h)

	callTool(t, queriesSearchHandler, c, map[string]any{
		"domain":      "example.com",
		"client_ip":   "192.168.1.10",
		"client_name": "desktop",
		"upstream":    "1.1.1.1",
		"type":        "AAAA",
		"status":      "GRAVITY",
		"reply":       "NXDOMAIN",
		"dnssec":      "SECURE",
		"from":        1700000000.0,
		"until":       1700003600.0,
		"length":      50.0,
		"cursor":      1234.0,
	})

	req := h.Only(t, "GET", "/queries")
	for key, want := range map[string]string{
		"domain":      "example.com",
		"client_ip":   "192.168.1.10",
		"client_name": "desktop",
		"upstream":    "1.1.1.1",
		"type":        "AAAA",
		"status":      "GRAVITY",
		"reply":       "NXDOMAIN",
		"dnssec":      "SECURE",
		"from":        "1700000000",
		"until":       "1700003600",
		"length":      "50",
		"cursor":      "1234",
	} {
		req.AssertQuery(t, key, want)
	}
}

// TestQueriesSearch_LengthIsCappedOnTheWire proves the 100-row cap is applied
// before the request goes out rather than after the reply comes back. The cap
// exists to stop one call filling the model's context with query log; trimming
// locally would still have made Pi-hole assemble and send the whole thing.
func TestQueriesSearch_LengthIsCappedOnTheWire(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries":         []any{},
			"cursor":          0,
			"recordsTotal":    0,
			"recordsFiltered": 0,
		},
	})
	c := newTestClient(t, h)

	callTool(t, queriesSearchHandler, c, map[string]any{"length": 5000.0})

	h.Only(t, "GET", "/queries").AssertQuery(t, "length", "100")
}

func TestQueriesSearch_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries": []any{
				map[string]any{
					"id": 1, "time": 1700000000.0, "type": "A", "domain": "example.com",
					"cname": nil, "status": "FORWARDED",
					"client":   map[string]any{"ip": "192.168.1.10", "name": "desktop"},
					"dnssec":   "INSECURE",
					"reply":    map[string]any{"type": "IP", "time": 5.2},
					"list_id":  nil,
					"upstream": "1.1.1.1#53",
				},
			},
			"cursor":          100,
			"recordsTotal":    50000,
			"recordsFiltered": 150,
		},
	}))

	text := callTool(t, queriesSearchHandler, c, map[string]any{"detail": "minimal"})
	if !strings.Contains(text, "1 of 150 queries.") {
		t.Errorf("expected count-only output, got: %s", text)
	}
	if !strings.Contains(text, "cursor=100") {
		t.Errorf("expected cursor in minimal output, got: %s", text)
	}
	// Minimal should not contain domain details.
	if strings.Contains(text, "example.com") {
		t.Errorf("minimal should not contain domain details, got: %s", text)
	}
}

func TestQueriesSearch_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries": []any{
				map[string]any{
					"id": 1, "time": 1700000000.0, "type": "A", "domain": "example.com",
					"cname": nil, "status": "FORWARDED",
					"client":   map[string]any{"ip": "192.168.1.10", "name": "desktop"},
					"dnssec":   "INSECURE",
					"reply":    map[string]any{"type": "IP", "time": 5.2},
					"list_id":  nil,
					"upstream": "1.1.1.1#53",
				},
			},
			"cursor":          0,
			"recordsTotal":    50000,
			"recordsFiltered": 1,
		},
	}))

	text := callTool(t, queriesSearchHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Time,Type,Domain,Status,Client,Upstream") {
		t.Errorf("expected CSV headers, got: %s", text)
	}
	if !strings.Contains(text, "example.com") {
		t.Errorf("expected domain in CSV row, got: %s", text)
	}
}

func TestQueriesSearch_Pagination(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries": []any{
				map[string]any{
					"id": 1, "time": 1700000000.0, "type": "A", "domain": "page1.com",
					"cname": nil, "status": "FORWARDED",
					"client":   map[string]any{"ip": "192.168.1.10", "name": "desktop"},
					"dnssec":   nil,
					"reply":    map[string]any{"type": "IP", "time": 3.1},
					"list_id":  nil,
					"upstream": "8.8.8.8#53",
				},
			},
			"cursor":          250,
			"recordsTotal":    50000,
			"recordsFiltered": 500,
		},
	}))

	text := callTool(t, queriesSearchHandler, c, nil)
	if !strings.Contains(text, "1 of 500 queries") {
		t.Errorf("expected '1 of 500 queries', got: %s", text)
	}
	if !strings.Contains(text, "cursor=250") {
		t.Errorf("expected next page cursor, got: %s", text)
	}
}

func TestQueriesSearch_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries":         []any{},
			"cursor":          0,
			"recordsTotal":    0,
			"recordsFiltered": 0,
		},
	}))

	text := callTool(t, queriesSearchHandler, c, map[string]any{"detail": "minimal"})
	if !strings.Contains(text, "0 of 0 queries.") {
		t.Errorf("expected '0 of 0 queries.', got: %s", text)
	}
}

func TestQueriesSuggestions_Normal(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/queries/suggestions": map[string]any{
			"suggestions": map[string]any{
				"domain":      []any{"example.com"},
				"client_ip":   []any{"192.168.1.10"},
				"client_name": []any{"desktop"},
				"upstream":    []any{"1.1.1.1#53"},
				"type":        []any{"A", "AAAA"},
				"status":      []any{"FORWARDED", "GRAVITY"},
				"reply":       []any{"IP", "NODATA"},
				"dnssec":      []any{"INSECURE"},
			},
		},
	})
	c := newTestClient(t, h)

	text := callTool(t, queriesSuggestionsHandler, c, nil)
	if !strings.Contains(text, "Types:") {
		t.Errorf("expected 'Types:' section, got: %s", text)
	}
	if !strings.Contains(text, "A, AAAA") {
		t.Errorf("expected query types, got: %s", text)
	}
	if !strings.Contains(text, "Statuses:") {
		t.Errorf("expected 'Statuses:' section, got: %s", text)
	}
	if !strings.Contains(text, "FORWARDED") {
		t.Errorf("expected status values, got: %s", text)
	}
	if !strings.Contains(text, "Upstreams:") {
		t.Errorf("expected 'Upstreams:' section, got: %s", text)
	}

	// Suggestions is a plain read of a sibling path of /queries. The route
	// table answers on path alone, so nothing here previously distinguished it
	// from a filtered query search that happened to render the same headings.
	req := h.Only(t, "GET", "/queries/suggestions")
	req.AssertNoQueryString(t)
}
