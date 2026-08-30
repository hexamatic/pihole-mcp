package tools

import (
	"strings"
	"testing"
)

func TestDomainsList_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/domains": map[string]any{
			"domains": []any{
				map[string]any{"domain": "example.com", "type": "deny", "kind": "exact", "enabled": true, "comment": "test", "id": 1, "groups": []any{0}, "date_added": 1700000000, "date_modified": 1700000000},
				map[string]any{"domain": "ads.net", "type": "deny", "kind": "regex", "enabled": false, "comment": "", "id": 2, "groups": []any{0, 1}, "date_added": 1700000000, "date_modified": 1700000000},
			},
		},
	}))

	text := callTool(t, domainsListHandler, c, nil)
	if !strings.Contains(text, "2 domains") {
		t.Errorf("expected domain count header, got: %s", text)
	}
	if !strings.Contains(text, "example.com") {
		t.Errorf("expected example.com in output, got: %s", text)
	}
	if !strings.Contains(text, "ads.net") {
		t.Errorf("expected ads.net in output, got: %s", text)
	}
}

func TestDomainsList_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/domains": map[string]any{
			"domains": []any{
				map[string]any{"domain": "example.com", "type": "deny", "kind": "exact", "enabled": true, "comment": "", "id": 1, "groups": []any{0}},
			},
		},
	}))

	text := callTool(t, domainsListHandler, c, map[string]any{"detail": "minimal"})
	if !strings.Contains(text, "1 domains.") {
		t.Errorf("expected single-line count, got: %s", text)
	}
}

func TestDomainsList_Full(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/domains": map[string]any{
			"domains": []any{
				map[string]any{"domain": "example.com", "type": "deny", "kind": "exact", "enabled": true, "comment": "", "id": 5, "groups": []any{0, 2}},
			},
		},
	}))

	text := callTool(t, domainsListHandler, c, map[string]any{"detail": "full"})
	if !strings.Contains(text, "id=5") {
		t.Errorf("expected id in full detail, got: %s", text)
	}
	if !strings.Contains(text, "groups=") {
		t.Errorf("expected groups in full detail, got: %s", text)
	}
}

func TestDomainsList_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/domains": map[string]any{
			"domains": []any{
				map[string]any{"domain": "example.com", "type": "deny", "kind": "exact", "enabled": true, "comment": "blocked", "id": 1, "groups": []any{0}},
			},
		},
	}))

	text := callTool(t, domainsListHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Domain,Type,Kind,Enabled,Comment") {
		t.Errorf("expected CSV header, got: %s", text)
	}
	if !strings.Contains(text, "example.com") {
		t.Errorf("expected domain in CSV row, got: %s", text)
	}
}

func TestDomainsList_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/domains": map[string]any{"domains": []any{}},
	}))

	text := callTool(t, domainsListHandler, c, nil)
	if text != "No domains found." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestDomainsAdd_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/domains/deny/exact": map[string]any{
			"domains":   []any{},
			"processed": map[string]any{"success": []any{map[string]any{"item": "example.com"}}, "errors": []any{}},
		},
	})
	c := newTestClient(t, rec)

	text := callTool(t, domainsAddHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com",
	})
	if !strings.Contains(text, "Domain added") {
		t.Errorf("expected 'Domain added' message, got: %s", text)
	}
	if !strings.Contains(text, "example.com") {
		t.Errorf("expected processed domain in output, got: %s", text)
	}

	// The list an entry lands on is chosen entirely by the path. A POST to
	// /domains/allow/exact blocks nothing and reports exactly the same
	// "Domain added" text, so the reply is no evidence at all.
	req := rec.Only(t, "POST", "/domains/deny/exact")
	req.AssertRawPath(t, "/domains/deny/exact")
	req.AssertNoQueryString(t)
	// An add sends comment and enabled every time rather than leaving FTL's
	// defaults to decide them, and the rules go out as an array: FTL rejects a
	// comma-joined string with 400 "Invalid domain".
	req.AssertBodyKeys(t, "domain", "comment", "enabled")
	req.AssertField(t, "domain", []any{"example.com"})
	req.AssertField(t, "enabled", true)
	// Without Content-Type the body is an unlabelled blob. pihole.Client sets
	// the header for every request that carries one.
	req.AssertHeader(t, "Content-Type", "application/json")
}

// The optional arguments have never reached a handler in any test, so the
// branches that build them are dead code under test: renaming body["comment"]
// to body["note"], or deleting it, survives the whole suite. A user who types
// a comment loses it silently, and an add that was meant to be disabled goes
// out enabled. AssertField pins the JSON type as well as the value, so sending
// the string "false" where the bool belongs is caught too.
//
// This is not the known comment-only-PUT defect: that one is on the update
// handlers. An add that sends an explicit enabled=false is correct today and
// stays correct after that fix.
func TestDomainsAdd_SendsCommentAndExplicitDisable(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/domains/deny/exact": map[string]any{
			"domains":   []any{},
			"processed": map[string]any{"success": []any{map[string]any{"item": "example.com"}}, "errors": []any{}},
		},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsAddHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com",
		"comment": "blocked by policy", "enabled": false,
	})

	req := rec.Only(t, "POST", "/domains/deny/exact")
	req.AssertBodyKeys(t, "domain", "comment", "enabled")
	req.AssertField(t, "domain", []any{"example.com"})
	req.AssertField(t, "comment", "blocked by policy")
	req.AssertField(t, "enabled", false)
	req.AssertNoQueryString(t)
}

func TestDomainsUpdate_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/domains/deny/exact/example.com": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, domainsUpdateHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com", "comment": "updated",
	})
	if !strings.Contains(text, "Updated") {
		t.Errorf("expected 'Updated' message, got: %s", text)
	}

	// Type, kind and domain are all path segments. Dropping or reordering any
	// one of them updates a different entry, or none, and the reply text is
	// identical either way.
	req := rec.Only(t, "PUT", "/domains/deny/exact/example.com")
	req.AssertRawPath(t, "/domains/deny/exact/example.com")
	req.AssertNoQueryString(t)
	req.AssertField(t, "comment", "updated")
	// The full key set is deliberately not pinned here. A comment-only update
	// sends no enabled key, which FTL reads as "enable this entry", so editing
	// the comment on a disabled domain silently re-enables it. Freezing that
	// shape would make the eventual fix look like the regression.
	// TestDomainsUpdate_ExplicitDisableSendsEnabledFalse pins the body that is
	// correct whichever way it is fixed.
}

// enabled only ever reaches the wire when it is false, so an explicit disable is
// the one update whose body is complete today. Pinning it guards the value going
// out as a JSON boolean rather than the string "false", which FTL accepts and
// then treats as truthy.
func TestDomainsUpdate_ExplicitDisableSendsEnabledFalse(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/domains/deny/exact/example.com": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsUpdateHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com",
		"comment": "paused", "enabled": false,
	})

	req := rec.Only(t, "PUT", "/domains/deny/exact/example.com")
	req.AssertBodyKeys(t, "comment", "enabled")
	req.AssertField(t, "enabled", false)
	req.AssertField(t, "comment", "paused")
}

func TestDomainsDelete_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/domains/deny/exact/example.com": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, domainsDeleteHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com",
	})
	if !strings.Contains(text, "Deleted") {
		t.Errorf("expected 'Deleted' message, got: %s", text)
	}

	// A delete that reached the API as anything but DELETE would leave the
	// entry in place while still reporting success.
	req := rec.Only(t, "DELETE", "/domains/deny/exact/example.com")
	req.AssertRawPath(t, "/domains/deny/exact/example.com")
	req.AssertNoQueryString(t)
	req.AssertNoBody(t)
}

func TestDomainsBatchDelete_Success(t *testing.T) {
	const items = `[{"item":"example.com","type":"deny","kind":"exact"}]`

	rec := piholeHandler(map[string]any{
		"/domains:batchDelete": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, domainsBatchDeleteHandler, c, map[string]any{"items": items})
	if !strings.Contains(text, "Batch delete completed") {
		t.Errorf("expected 'Batch delete completed' message, got: %s", text)
	}

	// The items string is handed to the client as pre-encoded JSON. If rawJSON
	// ever stopped implementing json.Marshaler the array would go out as a
	// quoted string, which FTL rejects, and the batch endpoint is one POST that
	// deletes an unbounded number of entries, so the body is the whole request.
	req := rec.Only(t, "POST", "/domains:batchDelete")
	req.AssertRawPath(t, "/domains:batchDelete")
	req.AssertNoQueryString(t)
	req.AssertRawBody(t, items)
}

func TestDomainsList_Error(t *testing.T) {
	c := newTestClient(t, piholeErrorServer(400, "bad_request", "Invalid filter", "Check parameters"))

	text := callToolExpectError(t, domainsListHandler, c, nil)
	if text == "" {
		t.Error("expected error text, got empty string")
	}
}

// ---------------------------------------------------------------------------
// Silent wrong writes
// ---------------------------------------------------------------------------

// FTL replaces both comment and enabled on every PUT: a body carrying only a
// comment re-enables a disabled rule, and one carrying only enabled nulls the
// comment. Verified against FTL v6.7. The tool therefore reads the entry back
// first and sends both fields, so editing one leaves the other alone.
func TestDomainsUpdate_CommentOnlyKeepsTheEntryDisabled(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /domains/deny/exact/example.com": map[string]any{"domains": []any{
			map[string]any{"domain": "example.com", "type": "deny", "kind": "exact",
				"comment": "paused for the holidays", "enabled": false},
		}},
		"PUT /domains/deny/exact/example.com": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsUpdateHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com", "comment": "edited",
	})

	req := rec.Only(t, "PUT", "/domains/deny/exact/example.com")
	req.AssertBodyKeys(t, "comment", "enabled")
	req.AssertField(t, "comment", "edited")
	req.AssertField(t, "enabled", false)
}

// The mirror image: disabling an entry must not wipe the comment that says why
// it exists.
func TestDomainsUpdate_EnabledOnlyKeepsTheComment(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /domains/deny/exact/example.com": map[string]any{"domains": []any{
			map[string]any{"domain": "example.com", "type": "deny", "kind": "exact",
				"comment": "tracker", "enabled": true},
		}},
		"PUT /domains/deny/exact/example.com": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsUpdateHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com", "enabled": false,
	})

	req := rec.Only(t, "PUT", "/domains/deny/exact/example.com")
	req.AssertBodyKeys(t, "comment", "enabled")
	req.AssertField(t, "comment", "tracker")
	req.AssertField(t, "enabled", false)
}

// A caller that supplies both fields replaces both, so there is nothing to
// carry over and no round trip to pay for.
func TestDomainsUpdate_BothFieldsSuppliedSkipsTheRead(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"PUT /domains/deny/exact/example.com": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsUpdateHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com",
		"comment": "paused", "enabled": false,
	})

	rec.AssertNone(t, "GET", "/domains/deny/exact/example.com")
	req := rec.Only(t, "PUT", "/domains/deny/exact/example.com")
	req.AssertField(t, "comment", "paused")
	req.AssertField(t, "enabled", false)
}

// FTL answers a lookup for a rule that does not exist with 200 and an empty
// array, and its PUT is an upsert. An update that creates the entry keeps
// working, with the tool's documented defaults.
func TestDomainsUpdate_MissingEntryStillWrites(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /domains/deny/exact/new.example.com": map[string]any{"domains": []any{}},
		"PUT /domains/deny/exact/new.example.com": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsUpdateHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "new.example.com", "comment": "fresh",
	})

	req := rec.Only(t, "PUT", "/domains/deny/exact/new.example.com")
	req.AssertField(t, "comment", "fresh")
	req.AssertField(t, "enabled", true)
}

// If the read fails there is no way to know what the PUT would reset, so the
// write must not happen at all. Reporting the read failure is the whole point:
// writing anyway is how the comment gets nulled.
func TestDomainsUpdate_ReadFailureLeavesTheEntryAlone(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"PUT /domains/deny/exact/example.com": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	text := callToolExpectError(t, domainsUpdateHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com", "comment": "edited",
	})
	if !strings.Contains(strings.ToLower(text), "read") {
		t.Errorf("error text does not say the read failed: %s", text)
	}
	rec.AssertNone(t, "PUT", "/domains/deny/exact/example.com")
}

// A regex rule containing '+' is the case that proves escaping. url.PathEscape
// leaves a plus alone because it is a legal sub-delimiter, but FTL reads it as
// a space: against FTL v6.7 this DELETE answered 404 with a literal '+' and
// 204 with %2B, on the same row. So a rule like this could be created and then
// never removed.
func TestDomainsDelete_EscapesReservedCharactersInTheDomain(t *testing.T) {
	const rule = `^ads[0-9]+\.example\.com`
	rec := piholeHandler(map[string]any{
		"DELETE /domains/deny/regex/" + rule: map[string]any{},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsDeleteHandler, c, map[string]any{
		"type": "deny", "kind": "regex", "domain": rule,
	})

	req := rec.Only(t, "DELETE", "/domains/deny/regex/"+rule)
	req.AssertRawPath(t, `/domains/deny/regex/%5Eads%5B0-9%5D%2B%5C.example%5C.com`)
}

func TestDomainsUpdate_EscapesReservedCharactersInTheDomain(t *testing.T) {
	const rule = `^ads[0-9]+\.example\.com`
	rec := piholeHandler(map[string]any{
		"GET /domains/deny/regex/" + rule: map[string]any{"domains": []any{}},
		"PUT /domains/deny/regex/" + rule: map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsUpdateHandler, c, map[string]any{
		"type": "deny", "kind": "regex", "domain": rule, "comment": "trackers",
	})

	rec.Only(t, "PUT", "/domains/deny/regex/"+rule).
		AssertRawPath(t, `/domains/deny/regex/%5Eads%5B0-9%5D%2B%5C.example%5C.com`)
}

// The description promises bulk add, and against a real Pi-hole the joined
// string was rejected outright: FTL answered 400 "Invalid domain" for
// {"domain":"a.example.com,b.example.com"} and created both rules for the
// array form.
func TestDomainsAdd_BulkSendsAnArray(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"POST /domains/deny/exact": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsAddHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "a.example.com, b.example.com",
	})

	req := rec.Only(t, "POST", "/domains/deny/exact")
	req.AssertField(t, "domain", []any{"a.example.com", "b.example.com"})
}

// A regex is never split on commas: they are significant inside a quantifier,
// so splitting ^ads{1,3}\.example\.com would send two rules, neither of which
// compiles.
func TestDomainsAdd_RegexIsNeverSplitOnCommas(t *testing.T) {
	const rule = `^ads{1,3}\.example\.com`
	rec := piholeHandler(map[string]any{
		"POST /domains/deny/regex": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsAddHandler, c, map[string]any{
		"type": "deny", "kind": "regex", "domain": rule,
	})

	rec.Only(t, "POST", "/domains/deny/regex").AssertField(t, "domain", []any{rule})
}

// Adding a rule must not leave FTL's own defaults to decide the enabled state.
func TestDomainsAdd_AlwaysSendsCommentAndEnabled(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"POST /domains/deny/exact": map[string]any{"domains": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, domainsAddHandler, c, map[string]any{
		"type": "deny", "kind": "exact", "domain": "example.com",
	})

	req := rec.Only(t, "POST", "/domains/deny/exact")
	req.AssertBodyKeys(t, "domain", "comment", "enabled")
	req.AssertField(t, "enabled", true)
	req.AssertField(t, "comment", "")
}
