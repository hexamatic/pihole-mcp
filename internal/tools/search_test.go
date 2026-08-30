package tools

import (
	"strings"
	"testing"
)

func TestSearchDomains_Found(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/search/ads.example.com": map[string]any{
			"search": map[string]any{
				"domains": []any{
					map[string]any{"domain": "ads.example.com", "type": "deny", "kind": "exact", "enabled": true, "comment": "", "id": 1, "date_added": 1700000000, "date_modified": 1700000000, "groups": []any{0}},
				},
				"gravity": []any{
					map[string]any{"domain": "ads.example.com", "address": "https://blocklist.example.com/list.txt", "comment": "", "enabled": true, "type": "block", "id": 1, "date_added": 1700000000, "date_modified": 1700000000, "date_updated": 1700000000, "number": 50000, "status": 0, "groups": []any{0}},
				},
				"parameters": map[string]any{"partial": false, "N": 20, "domain": "ads.example.com", "debug": false},
				"results": map[string]any{
					"domains": map[string]any{"exact": 1, "regex": 0},
					"gravity": map[string]any{"allow": 0, "block": 1},
					"total":   2,
				},
			},
		},
	})
	c := newTestClient(t, h)

	text := callTool(t, searchDomainsHandler, c, map[string]any{
		"domain": "ads.example.com",
	})
	if !strings.Contains(text, "2 matches") {
		t.Errorf("expected '2 matches', got: %s", text)
	}
	if !strings.Contains(text, "Domain list matches") {
		t.Errorf("expected domain list section, got: %s", text)
	}
	if !strings.Contains(text, "Gravity matches") {
		t.Errorf("expected gravity section, got: %s", text)
	}
	if !strings.Contains(text, "ads.example.com") {
		t.Errorf("expected domain name in output, got: %s", text)
	}
	if !strings.Contains(text, "blocklist.example.com") {
		t.Errorf("expected gravity source in output, got: %s", text)
	}

	// The domain is interpolated straight into the path, and the handler echoes
	// the domain it was given back into the heading rather than the one the API
	// answered about. Asking about the wrong domain therefore reads as a
	// perfectly ordinary answer about the right one, which is the failure mode
	// that matters here: this tool exists to be consulted before someone edits
	// a blocklist.
	req := h.Only(t, "GET", "")
	req.AssertRawPath(t, "/search/ads.example.com")
	// N caps each result category. Dropping it would silently fall back to
	// Pi-hole's own default rather than the 20 the tool advertises.
	req.AssertQuery(t, "N", "20")
	// partial defaults to false at both ends, so it is omitted rather than
	// sent as false. Sending partial=true by accident turns an exact-match
	// check into a substring sweep that reports unrelated entries as hits.
	req.AssertNoQuery(t, "partial")
}

// TestSearchDomains_ForwardsMaxResults pins max_results to the API's N, which
// is one of the few argument renames in the package. A dropped cap quietly
// truncates or inflates what the model sees, and the rendered output does not
// name the limit that was applied.
func TestSearchDomains_ForwardsMaxResults(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/search/ads.example.com": map[string]any{
			"search": map[string]any{
				"domains":    []any{},
				"gravity":    []any{},
				"parameters": map[string]any{"partial": false, "N": 5, "domain": "ads.example.com", "debug": false},
				"results": map[string]any{
					"domains": map[string]any{"exact": 0, "regex": 0},
					"gravity": map[string]any{"allow": 0, "block": 0},
					"total":   0,
				},
			},
		},
	})
	c := newTestClient(t, h)

	callTool(t, searchDomainsHandler, c, map[string]any{
		"domain":      "ads.example.com",
		"max_results": 5.0,
	})

	h.Only(t, "GET", "").AssertQuery(t, "N", "5")
}

func TestSearchDomains_NotFound(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/search/nonexistent.com": map[string]any{
			"search": map[string]any{
				"domains":    []any{},
				"gravity":    []any{},
				"parameters": map[string]any{"partial": false, "N": 20, "domain": "nonexistent.com", "debug": false},
				"results": map[string]any{
					"domains": map[string]any{"exact": 0, "regex": 0},
					"gravity": map[string]any{"allow": 0, "block": 0},
					"total":   0,
				},
			},
		},
	}))

	text := callTool(t, searchDomainsHandler, c, map[string]any{
		"domain": "nonexistent.com",
	})
	if !strings.Contains(text, "0 matches") {
		t.Errorf("expected '0 matches', got: %s", text)
	}
	if strings.Contains(text, "Domain list matches") {
		t.Errorf("should not show domain list section when empty, got: %s", text)
	}
	if strings.Contains(text, "Gravity matches") {
		t.Errorf("should not show gravity section when empty, got: %s", text)
	}
}

func TestSearchDomains_Partial(t *testing.T) {
	h := piholeHandler(map[string]any{
		"/search/ads.example.com": map[string]any{
			"search": map[string]any{
				"domains": []any{
					map[string]any{"domain": "ads.example.com", "type": "deny", "kind": "exact", "enabled": true, "comment": "", "id": 1, "date_added": 1700000000, "date_modified": 1700000000, "groups": []any{0}},
				},
				"gravity":    []any{},
				"parameters": map[string]any{"partial": true, "N": 20, "domain": "ads.example.com", "debug": false},
				"results": map[string]any{
					"domains": map[string]any{"exact": 1, "regex": 0},
					"gravity": map[string]any{"allow": 0, "block": 0},
					"total":   1,
				},
			},
		},
	})
	c := newTestClient(t, h)

	text := callTool(t, searchDomainsHandler, c, map[string]any{
		"domain":  "ads.example.com",
		"partial": true,
	})
	if !strings.Contains(text, "1 matches") {
		t.Errorf("expected '1 matches', got: %s", text)
	}
	if !strings.Contains(text, "1 exact") {
		t.Errorf("expected exact match count, got: %s", text)
	}

	// Partial matching is what makes this call answer a different question, and
	// only the query string carries it. The fixture's own parameters block is
	// canned, so the rendered output looks the same either way.
	h.Only(t, "GET", "").AssertQuery(t, "partial", "true")
}
