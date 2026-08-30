package tools

import (
	"strings"
	"testing"
)

func TestListsList_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/lists": map[string]any{
			"lists": []any{
				map[string]any{"address": "https://example.com/list.txt", "type": "block", "comment": "Ad list", "groups": []any{0}, "enabled": true, "id": 1, "number": 50000, "invalid_domains": 3, "date_added": 1700000000, "date_modified": 1700000000, "date_updated": 1700000000, "abp_entries": 0, "status": 0},
				map[string]any{"address": "https://example.com/allow.txt", "type": "allow", "comment": "", "groups": []any{0}, "enabled": true, "id": 2, "number": 100, "invalid_domains": 0, "date_added": 1700000000, "date_modified": 1700000000, "date_updated": 1700000000, "abp_entries": 0, "status": 0},
			},
		},
	}))

	text := callTool(t, listsListHandler, c, nil)
	if !strings.Contains(text, "2 lists") {
		t.Errorf("expected list count, got: %s", text)
	}
	if !strings.Contains(text, "50,000 domains") {
		t.Errorf("expected formatted domain count, got: %s", text)
	}
	if !strings.Contains(text, "https://example.com/list.txt") {
		t.Errorf("expected list URL, got: %s", text)
	}
}

func TestListsList_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/lists": map[string]any{
			"lists": []any{
				map[string]any{"address": "https://example.com/list.txt", "type": "block", "comment": "", "groups": []any{0}, "enabled": true, "id": 1, "number": 50000, "invalid_domains": 0, "date_added": 1700000000, "date_modified": 1700000000, "date_updated": 1700000000, "abp_entries": 0, "status": 0},
			},
		},
	}))

	text := callTool(t, listsListHandler, c, map[string]any{"detail": "minimal"})
	if !strings.Contains(text, "1 lists.") {
		t.Errorf("expected minimal count, got: %s", text)
	}
}

func TestListsList_Full(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/lists": map[string]any{
			"lists": []any{
				map[string]any{"address": "https://example.com/list.txt", "type": "block", "comment": "", "groups": []any{0}, "enabled": true, "id": 7, "number": 50000, "invalid_domains": 3, "date_added": 1700000000, "date_modified": 1700000000, "date_updated": 1700000000, "abp_entries": 0, "status": 0},
			},
		},
	}))

	text := callTool(t, listsListHandler, c, map[string]any{"detail": "full"})
	if !strings.Contains(text, "id=7") {
		t.Errorf("expected id in full detail, got: %s", text)
	}
	if !strings.Contains(text, "updated=") {
		t.Errorf("expected updated timestamp in full detail, got: %s", text)
	}
	if !strings.Contains(text, "invalid=3") {
		t.Errorf("expected invalid count in full detail, got: %s", text)
	}
}

func TestListsList_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/lists": map[string]any{
			"lists": []any{
				map[string]any{"address": "https://example.com/list.txt", "type": "block", "comment": "Ad list", "groups": []any{0}, "enabled": true, "id": 1, "number": 50000, "invalid_domains": 0, "date_added": 1700000000, "date_modified": 1700000000, "date_updated": 1700000000, "abp_entries": 0, "status": 0},
			},
		},
	}))

	text := callTool(t, listsListHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Address,Type,Domains,Enabled,Comment") {
		t.Errorf("expected CSV headers, got: %s", text)
	}
	if !strings.Contains(text, "https://example.com/list.txt") {
		t.Errorf("expected list address in CSV, got: %s", text)
	}
}

func TestListsList_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/lists": map[string]any{"lists": []any{}},
	}))

	text := callTool(t, listsListHandler, c, nil)
	if text != "No lists found." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestListsAdd_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/lists": map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, listsAddHandler, c, map[string]any{
		"address": "https://example.com/new.txt", "type": "block",
	})
	if !strings.Contains(text, "Added") {
		t.Errorf("expected 'Added' message, got: %s", text)
	}
	if !strings.Contains(text, "gravity") {
		t.Errorf("expected gravity reminder, got: %s", text)
	}

	req := rec.Only(t, "POST", "/lists")
	req.AssertRawPath(t, "/lists")
	// type is the only parameter this endpoint takes. AssertNoQueryString would
	// be wrong here, so pin the key set instead: a stray extra parameter is as
	// invisible in the reply as a missing one.
	req.AssertQueryKeys(t, "type")
	// POST /api/lists takes type as a query parameter and rejects it in the
	// body with a 400. This is the single most surprising thing about the
	// endpoint and nothing but the wire shows which side of the request it
	// went to: an allow list added as a block list looks identical in the
	// reply, and quietly blocks everything it was meant to permit.
	req.AssertQuery(t, "type", "block")
	// An add sends comment and enabled every time rather than leaving FTL's
	// defaults to decide them.
	req.AssertBodyKeys(t, "address", "comment", "enabled")
	req.AssertField(t, "address", "https://example.com/new.txt")
	req.AssertField(t, "enabled", true)
}

// Mirror of the domains case, plus the type parameter. A list added with a
// comment and an explicit disable is the only add whose body carries all three
// keys, and none of them is visible in the reply text.
func TestListsAdd_SendsCommentAndExplicitDisable(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/lists": map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, listsAddHandler, c, map[string]any{
		"address": "https://example.com/new.txt", "type": "block",
		"comment": "vendor supplied", "enabled": false,
	})

	req := rec.Only(t, "POST", "/lists")
	req.AssertBodyKeys(t, "address", "comment", "enabled")
	req.AssertField(t, "address", "https://example.com/new.txt")
	req.AssertField(t, "comment", "vendor supplied")
	req.AssertField(t, "enabled", false)
	req.AssertQueryKeys(t, "type")
	req.AssertQuery(t, "type", "block")
}

func TestListsUpdate_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/lists/https://example.com/list.txt": map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, listsUpdateHandler, c, map[string]any{
		"address": "https://example.com/list.txt", "type": "block", "comment": "updated",
	})
	if !strings.Contains(text, "Updated") {
		t.Errorf("expected 'Updated' message, got: %s", text)
	}
	if !strings.Contains(text, "https://example.com/list.txt") {
		t.Errorf("expected list address in message, got: %s", text)
	}

	// Selecting on the decoded path asserts that the address reached the API as
	// a path segment. The raw form is deliberately not pinned: the address is
	// concatenated into the path unescaped today, and pinning that spelling
	// would turn the escaping fix into a test failure. The decoded path is the
	// same either way.
	req := rec.Only(t, "PUT", "/lists/https://example.com/list.txt")
	req.AssertQueryKeys(t, "type")
	// Unlike the add, the update carries type in the query and in the body.
	// Sending only one of the two silently converts a block list to an allow
	// list, or fails to convert one that was meant to change.
	req.AssertQuery(t, "type", "block")
	req.AssertField(t, "type", "block")
	req.AssertField(t, "comment", "updated")
	// The full key set is deliberately not pinned here. A comment-only update
	// sends no enabled key, which FTL reads as "enable this list", so editing
	// the comment on a disabled list silently re-enables it. Freezing that
	// shape would make the eventual fix look like the regression.
}

// enabled only ever reaches the wire when it is false, so an explicit disable is
// the one list update whose body is complete today. It is also the only place
// the three-key body is visible: type is always sent, comment only when given.
func TestListsUpdate_ExplicitDisableSendsEnabledFalse(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/lists/https://example.com/list.txt": map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, listsUpdateHandler, c, map[string]any{
		"address": "https://example.com/list.txt", "type": "block",
		"comment": "paused", "enabled": false,
	})

	req := rec.Only(t, "PUT", "/lists/https://example.com/list.txt")
	req.AssertQueryKeys(t, "type")
	req.AssertBodyKeys(t, "comment", "enabled", "type")
	req.AssertField(t, "enabled", false)
	req.AssertField(t, "type", "block")
}

func TestListsDelete_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/lists/https://example.com/list.txt": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, listsDeleteHandler, c, map[string]any{
		"address": "https://example.com/list.txt", "type": "block",
	})
	if !strings.Contains(text, "Deleted") {
		t.Errorf("expected 'Deleted' message, got: %s", text)
	}

	// The same address exists once per type, so dropping the type parameter
	// deletes the wrong list or nothing at all. As with the update, only the
	// decoded path is pinned, because the escaping of the address is a known
	// defect that a later change will alter.
	req := rec.Only(t, "DELETE", "/lists/https://example.com/list.txt")
	req.AssertQuery(t, "type", "block")
	req.AssertQueryKeys(t, "type")
	req.AssertNoBody(t)
}

func TestListsBatchDelete_Success(t *testing.T) {
	const items = `[{"item":"https://example.com/list.txt","type":"block"}]`

	rec := piholeHandler(map[string]any{
		"/lists:batchDelete": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, listsBatchDeleteHandler, c, map[string]any{"items": items})
	if !strings.Contains(text, "Batch delete completed") {
		t.Errorf("expected 'Batch delete completed' message, got: %s", text)
	}

	// Every item carries its own type, so unlike the single-list writes this
	// endpoint takes no type parameter. One copied across from listsDelete
	// would apply to the whole batch.
	req := rec.Only(t, "POST", "/lists:batchDelete")
	req.AssertRawPath(t, "/lists:batchDelete")
	req.AssertNoQueryString(t)
	req.AssertRawBody(t, items)
}

// ---------------------------------------------------------------------------
// Silent wrong writes
// ---------------------------------------------------------------------------

// A list PUT is a full replacement of comment and enabled, exactly as for
// domains and groups. Verified against FTL v6.7: a comment-only PUT of a
// disabled list read back enabled.
func TestListsUpdate_CommentOnlyKeepsTheListDisabled(t *testing.T) {
	const addr = "https://lists.example.com/ads.txt"
	rec := piholeHandler(map[string]any{
		"GET /lists/" + addr: map[string]any{"lists": []any{
			map[string]any{"address": addr, "type": "block", "comment": "off while testing", "enabled": false},
		}},
		"PUT /lists/" + addr: map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, listsUpdateHandler, c, map[string]any{
		"address": addr, "type": "block", "comment": "edited",
	})

	req := rec.Only(t, "PUT", "/lists/"+addr)
	req.AssertField(t, "comment", "edited")
	req.AssertField(t, "enabled", false)
	req.AssertQuery(t, "type", "block")
}

func TestListsUpdate_EnabledOnlyKeepsTheComment(t *testing.T) {
	const addr = "https://lists.example.com/ads.txt"
	rec := piholeHandler(map[string]any{
		"GET /lists/" + addr: map[string]any{"lists": []any{
			map[string]any{"address": addr, "type": "block", "comment": "nightly refresh", "enabled": true},
		}},
		"PUT /lists/" + addr: map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, listsUpdateHandler, c, map[string]any{
		"address": addr, "type": "block", "enabled": false,
	})

	req := rec.Only(t, "PUT", "/lists/"+addr)
	req.AssertField(t, "comment", "nightly refresh")
	req.AssertField(t, "enabled", false)
}

// A blocklist URL carries a query string of its own often enough to matter,
// and splicing it into the path raw ends the path at the '?': everything after
// it becomes the request's query, so the update lands on a different row or on
// none, and the reply says "Updated" either way.
func TestListsUpdate_EscapesAQueryStringInTheAddress(t *testing.T) {
	const addr = "https://lists.example.com/ads.txt?token=abc123"
	rec := piholeHandler(map[string]any{
		"GET /lists/" + addr: map[string]any{"lists": []any{}},
		"PUT /lists/" + addr: map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, listsUpdateHandler, c, map[string]any{
		"address": addr, "type": "block", "comment": "edited",
	})

	req := rec.Only(t, "PUT", "/lists/"+addr)
	req.AssertRawPath(t, "/lists/https:%2F%2Flists.example.com%2Fads.txt%3Ftoken=abc123")
	req.AssertQuery(t, "type", "block")
	req.AssertNoQuery(t, "token")
}

func TestListsDelete_EscapesAQueryStringInTheAddress(t *testing.T) {
	const addr = "https://lists.example.com/ads.txt?token=abc123"
	rec := piholeHandler(map[string]any{
		"DELETE /lists/" + addr: map[string]any{},
	})
	c := newTestClient(t, rec)

	callTool(t, listsDeleteHandler, c, map[string]any{"address": addr, "type": "block"})

	req := rec.Only(t, "DELETE", "/lists/"+addr)
	req.AssertRawPath(t, "/lists/https:%2F%2Flists.example.com%2Fads.txt%3Ftoken=abc123")
	req.AssertQuery(t, "type", "block")
	req.AssertNoQuery(t, "token")
}

func TestListsAdd_AlwaysSendsCommentAndEnabled(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"POST /lists": map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, listsAddHandler, c, map[string]any{
		"address": "https://lists.example.com/ads.txt", "type": "block",
	})

	req := rec.Only(t, "POST", "/lists")
	req.AssertBodyKeys(t, "address", "comment", "enabled")
	req.AssertField(t, "enabled", true)
}

func TestListsUpdate_ReadFailureLeavesTheListAlone(t *testing.T) {
	const addr = "https://lists.example.com/ads.txt"
	rec := piholeHandler(map[string]any{
		"PUT /lists/" + addr: map[string]any{"lists": []any{}},
	})
	c := newTestClient(t, rec)

	callToolExpectError(t, listsUpdateHandler, c, map[string]any{
		"address": addr, "type": "block", "comment": "edited",
	})
	rec.AssertNone(t, "PUT", "/lists/"+addr)
}
