package tools

import (
	"fmt"
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
	// "0 of 0 queries" is indistinguishable from a Pi-hole that has no data at
	// all. The usual cause is a range the search never reached, so say so.
	if !strings.Contains(text, "No queries matched") {
		t.Errorf("expected a named empty result, got: %s", text)
	}
	if !strings.Contains(text, "disk=true") {
		t.Errorf("expected the long-term database hint, got: %s", text)
	}
}

// An empty result has to name how far back the data goes, or a caller cannot
// tell "nothing matched" from "you searched before the database starts". FTL
// returns both floors on the search response itself, verified against v6.7.
func TestQueriesSearch_EmptyNamesTheEarliestDate(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries": []any{}, "cursor": 0, "recordsTotal": 0, "recordsFiltered": 0,
			"earliest_timestamp":      1783733685.9495127,
			"earliest_timestamp_disk": 1783600000,
		},
	}))

	text := callTool(t, queriesSearchHandler, c, nil)
	if !strings.Contains(text, "in-memory window only goes back to") {
		t.Errorf("expected the in-memory floor, got: %s", text)
	}

	disk := callTool(t, queriesSearchHandler, c, map[string]any{"disk": true})
	if !strings.Contains(disk, "long-term database only goes back to") {
		t.Errorf("expected the on-disk floor when disk=true, got: %s", disk)
	}
	if strings.Contains(disk, "Set disk=true") {
		t.Errorf("should not suggest disk=true when it was already set, got: %s", disk)
	}
}

// FTL reports 0 for the on-disk floor until queries have been flushed, which
// is not the epoch and must not be rendered as a date.
func TestQueriesSearch_EmptyDiskNotYetFlushed(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries": map[string]any{
			"queries": []any{}, "cursor": 0, "recordsTotal": 0, "recordsFiltered": 0,
			"earliest_timestamp":      1783733685.9495127,
			"earliest_timestamp_disk": 0,
		},
	}))

	text := callTool(t, queriesSearchHandler, c, map[string]any{"disk": true})
	if !strings.Contains(text, "not flushed any queries to disk") {
		t.Errorf("expected the not-yet-flushed explanation, got: %s", text)
	}
	if strings.Contains(text, "1970") {
		t.Errorf("rendered the zero floor as a date: %s", text)
	}
}

// FTL returns an empty clients map while still bucketing every slot's activity
// in the series, so a renderer reading only the map reports no clients while
// holding a full day of data. Verified against FTL v6.7.
func TestHistoryClients_DerivedFromSeries(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{},
			"history": []any{
				map[string]any{"timestamp": 1700000000, "data": map[string]any{"others": 0}},
				map[string]any{"timestamp": 1700000600, "data": map[string]any{"others": 27}},
			},
		},
	}))

	text := callTool(t, historyClientsHandler, c, nil)
	if !strings.Contains(text, "**1 clients:**") {
		t.Errorf("expected the series-only client to be counted, got: %s", text)
	}
	if !strings.Contains(text, "- others — 27 queries") {
		t.Errorf("expected the summed total from the series, got: %s", text)
	}
}

// FTL only reads the long-term database when asked. Without disk on the wire a
// historical range silently searches the in-memory window instead.
func TestQueriesSearch_ForwardsDisk(t *testing.T) {
	routes := map[string]any{
		"/queries": map[string]any{
			"queries": []any{}, "cursor": 0, "recordsTotal": 0, "recordsFiltered": 0,
		},
		"/info/database": map[string]any{"earliest_timestamp": 1783733685.0},
	}

	on := piholeHandler(routes)
	callTool(t, queriesSearchHandler, newTestClient(t, on), map[string]any{"disk": true, "from": 1700000000})
	on.Only(t, "GET", "/queries").AssertQuery(t, "disk", "true")

	off := piholeHandler(routes)
	callTool(t, queriesSearchHandler, newTestClient(t, off), map[string]any{"from": 1700000000})
	off.Only(t, "GET", "/queries").AssertNoQuery(t, "disk")
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

// The filter values are user data and reach FTL through format.QueryParams.
// An upstream is routinely written host#port, and spliced in raw the '#' ended
// the request at the fragment: FTL was asked about 8.8.8.8, length never
// arrived, and the tool reported the answer as though it were the one
// requested.
func TestQueriesSearch_EscapesFilterValues(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/queries": map[string]any{"queries": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, queriesSearchHandler, c, map[string]any{
		"upstream": "8.8.8.8#53", "length": float64(3),
	})

	req := rec.Only(t, "GET", "/queries")
	req.AssertQuery(t, "upstream", "8.8.8.8#53")
	req.AssertQuery(t, "length", "3")
	if req.RawQuery != "length=3&upstream=8.8.8.8%2353" {
		t.Errorf("RawQuery = %q, want length=3&upstream=8.8.8.8%%2353", req.RawQuery)
	}
}

// A filter value cannot smuggle a second parameter into the request.
func TestQueriesSearch_FilterValueCannotAddAParameter(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/queries": map[string]any{"queries": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, queriesSearchHandler, c, map[string]any{
		"domain": "example.com&length=9999", "length": float64(3),
	})

	req := rec.Only(t, "GET", "/queries")
	req.AssertQueryKeys(t, "domain", "length")
	req.AssertQuery(t, "length", "3")
	req.AssertQuery(t, "domain", "example.com&length=9999")
}

// The tool's description leads with "known domains", and the handler listed
// every category except domains and client names, so the one thing a caller
// most needs before filtering was the one thing never returned.
func TestQueriesSuggestions_ListsDomainsAndClientNames(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries/suggestions": map[string]any{
			"suggestions": map[string]any{
				"domain":      []any{"github.com", "adservice.google.com"},
				"client_ip":   []any{"127.0.0.1"},
				"client_name": []any{"localhost"},
				"upstream":    []any{"cache"},
				"type":        []any{"A"},
				"status":      []any{"GRAVITY"},
				"reply":       []any{"NODATA"},
				"dnssec":      []any{"SECURE"},
			},
		},
	}))

	text := callTool(t, queriesSuggestionsHandler, c, nil)
	for _, want := range []string{
		"**Domains:** github.com, adservice.google.com",
		"**Clients:** 127.0.0.1",
		"**Client names:** localhost",
		"**Upstreams:** cache",
		"**Types:** A",
		"**Statuses:** GRAVITY",
		"**Replies:** NODATA",
		"**DNSSEC:** SECURE",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

// A busy Pi-hole knows tens of thousands of domains, so each category is
// bounded and says when it has been.
func TestQueriesSuggestions_TruncatesAndSaysSo(t *testing.T) {
	domains := make([]any, 0, 120)
	for i := range 120 {
		domains = append(domains, fmt.Sprintf("d%03d.example.com", i))
	}
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries/suggestions": map[string]any{
			"suggestions": map[string]any{"domain": domains},
		},
	}))

	text := callTool(t, queriesSuggestionsHandler, c, nil)
	if !strings.Contains(text, "(50 of 120; raise limit for more)") {
		t.Errorf("expected a truncation note, got: %s", text)
	}
	if strings.Contains(text, "d050.example.com") {
		t.Errorf("emitted an entry past the limit: %s", text)
	}

	raised := callTool(t, queriesSuggestionsHandler, c, map[string]any{"limit": 120})
	if strings.Contains(raised, "raise limit for more") {
		t.Errorf("expected no truncation note at limit=120, got: %s", raised)
	}
	if !strings.Contains(raised, "d119.example.com") {
		t.Errorf("expected the last entry at limit=120, got: %s", raised)
	}
}

func TestQueriesSuggestions_NegativeLimitIsNamedError(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/queries/suggestions": map[string]any{"suggestions": map[string]any{"domain": []any{"a.example.com"}}},
	}))

	msg := callToolExpectError(t, queriesSuggestionsHandler, c, map[string]any{"limit": 0})
	if !strings.Contains(msg, "'limit'") {
		t.Errorf("error %q does not name the parameter", msg)
	}
}
