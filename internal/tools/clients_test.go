package tools

import (
	"strings"
	"testing"
)

func TestClientsList_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/clients": map[string]any{
			"clients": []any{
				map[string]any{"client": "192.168.1.10", "name": "desktop", "comment": "Main PC", "groups": []any{0, 1}, "id": 1},
				map[string]any{"client": "192.168.1.20", "name": "laptop", "comment": "", "groups": []any{0}, "id": 2},
			},
		},
	}))

	text := callTool(t, clientsListHandler, c, nil)
	if !strings.Contains(text, "2 clients") {
		t.Errorf("expected client count, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected first client IP, got: %s", text)
	}
	if !strings.Contains(text, "desktop") {
		t.Errorf("expected client name, got: %s", text)
	}
}

func TestClientsList_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/clients": map[string]any{
			"clients": []any{
				map[string]any{"client": "192.168.1.10", "name": "desktop", "comment": "Main PC", "groups": []any{0}, "id": 1},
			},
		},
	}))

	text := callTool(t, clientsListHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Client,Name,Comment,Groups") {
		t.Errorf("expected CSV headers, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected client in CSV, got: %s", text)
	}
}

func TestClientsList_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/clients": map[string]any{"clients": []any{}},
	}))

	text := callTool(t, clientsListHandler, c, nil)
	if text != "No configured clients." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestClientsSuggestions_Normal(t *testing.T) {
	hwaddr := "AA:BB:CC:DD:EE:FF"
	vendor := "Apple"
	addr := "192.168.1.50"
	names := "macbook"

	c := newTestClient(t, piholeHandler(map[string]any{
		"/clients/_suggestions": map[string]any{
			"clients": []any{
				map[string]any{"hwaddr": hwaddr, "macVendor": vendor, "lastQuery": 1700000000, "addresses": addr, "names": names},
				map[string]any{"lastQuery": 1700000000, "addresses": "10.0.0.5"},
			},
		},
	}))

	text := callTool(t, clientsSuggestionsHandler, c, nil)
	if !strings.Contains(text, "2 unconfigured clients") {
		t.Errorf("expected suggestion count, got: %s", text)
	}
	if !strings.Contains(text, "AA:BB:CC:DD:EE:FF") {
		t.Errorf("expected MAC address, got: %s", text)
	}
	if !strings.Contains(text, "Apple") {
		t.Errorf("expected vendor, got: %s", text)
	}
	if !strings.Contains(text, "unknown MAC") {
		t.Errorf("expected 'unknown MAC' for nil hwaddr, got: %s", text)
	}
}

func TestClientsSuggestions_NilFields(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/clients/_suggestions": map[string]any{
			"clients": []any{
				map[string]any{"lastQuery": 1700000000},
			},
		},
	}))

	text := callTool(t, clientsSuggestionsHandler, c, nil)
	if !strings.Contains(text, "unknown MAC") {
		t.Errorf("expected 'unknown MAC' for nil hwaddr, got: %s", text)
	}
	if !strings.Contains(text, "no IPs") {
		t.Errorf("expected 'no IPs' for nil addresses, got: %s", text)
	}
}

func TestClientsSuggestions_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/clients/_suggestions": map[string]any{"clients": []any{}},
	}))

	text := callTool(t, clientsSuggestionsHandler, c, nil)
	if text != "No unconfigured clients found." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestClientsAdd_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/clients": map[string]any{"clients": []any{}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, clientsAddHandler, c, map[string]any{"client": "192.168.1.30"})
	if !strings.Contains(text, "Added") {
		t.Errorf("expected 'Added' message, got: %s", text)
	}

	// The identifier goes in the body on an add and in the path on an update or
	// a delete, which is easy to get the wrong way round and impossible to see
	// in the confirmation text, because that is built from the argument.
	req := rec.Only(t, "POST", "/clients")
	req.AssertRawPath(t, "/clients")
	req.AssertNoQueryString(t)
	req.AssertBodyKeys(t, "client", "comment")
	req.AssertField(t, "client", "192.168.1.30")
}

// A client has no enabled field, so comment is the whole optional surface, and
// it has never reached the handler in any test.
func TestClientsAdd_SendsComment(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/clients": map[string]any{"clients": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, clientsAddHandler, c, map[string]any{
		"client": "192.168.1.50", "comment": "kids tablet",
	})

	req := rec.Only(t, "POST", "/clients")
	req.AssertBodyKeys(t, "client", "comment")
	req.AssertField(t, "client", "192.168.1.50")
	req.AssertField(t, "comment", "kids tablet")
	req.AssertNoQueryString(t)
}

func TestClientsUpdate_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/clients/192.168.1.10": map[string]any{"clients": []any{}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, clientsUpdateHandler, c, map[string]any{"client": "192.168.1.10", "comment": "updated"})
	if !strings.Contains(text, "Updated") {
		t.Errorf("expected 'Updated' message, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected client identifier in message, got: %s", text)
	}

	// A client has no enabled flag, so unlike the domain, group and list
	// updates this body is complete and its whole key set can be pinned. The
	// identifier must not be repeated in the body: FTL takes it from the path,
	// and a client key here would read as a request to rename the entry.
	req := rec.Only(t, "PUT", "/clients/192.168.1.10")
	req.AssertRawPath(t, "/clients/192.168.1.10")
	req.AssertNoQueryString(t)
	req.AssertBodyKeys(t, "comment")
	req.AssertField(t, "comment", "updated")
}

func TestClientsDelete_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/clients/192.168.1.30": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, clientsDeleteHandler, c, map[string]any{"client": "192.168.1.30"})
	if !strings.Contains(text, "Deleted") {
		t.Errorf("expected 'Deleted' message, got: %s", text)
	}

	// Deleting a client returns it to the default group, so a delete that went
	// to the collection rather than the entry would take out every client while
	// still reporting one identifier as removed.
	req := rec.Only(t, "DELETE", "/clients/192.168.1.30")
	req.AssertRawPath(t, "/clients/192.168.1.30")
	req.AssertNoQueryString(t)
	req.AssertNoBody(t)
}

func TestClientsBatchDelete_Success(t *testing.T) {
	const items = `["192.168.1.10","192.168.1.20"]`

	rec := piholeHandler(map[string]any{
		"/clients:batchDelete": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, clientsBatchDeleteHandler, c, map[string]any{"items": items})
	if !strings.Contains(text, "Batch delete completed") {
		t.Errorf("expected 'Batch delete completed' message, got: %s", text)
	}

	// The items string is handed to the client as pre-encoded JSON, so every
	// identifier in the batch has to survive the round trip verbatim. The reply
	// text is a constant and reports success for an empty batch just as loudly.
	req := rec.Only(t, "POST", "/clients:batchDelete")
	req.AssertRawPath(t, "/clients:batchDelete")
	req.AssertNoQueryString(t)
	req.AssertRawBody(t, items)
}

// A client identifier is free-form: Pi-hole accepts an IP, a MAC, a hostname
// or an interface name, so it can carry characters the path treats as
// structure. Unescaped, the request reaches a different row or none.
func TestClientsUpdate_EscapesReservedCharactersInTheClient(t *testing.T) {
	const client = "eth0#lan"
	rec := piholeHandler(map[string]any{
		"PUT /clients/" + client: map[string]any{"clients": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, clientsUpdateHandler, c, map[string]any{"client": client, "comment": "edited"})

	rec.Only(t, "PUT", "/clients/"+client).AssertRawPath(t, "/clients/eth0%23lan")
}

func TestClientsDelete_EscapesReservedCharactersInTheClient(t *testing.T) {
	const client = "eth0#lan"
	rec := piholeHandler(map[string]any{"DELETE /clients/" + client: map[string]any{}})
	c := newTestClient(t, rec)

	callTool(t, clientsDeleteHandler, c, map[string]any{"client": client})

	rec.Only(t, "DELETE", "/clients/"+client).AssertRawPath(t, "/clients/eth0%23lan")
}

// A client row has no enabled column, so its update is comment-only and needs
// no read-back: FTL preserves group membership across a PUT that omits it.
func TestClientsUpdate_SendsCommentAndNothingElse(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"PUT /clients/192.168.1.50": map[string]any{"clients": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, clientsUpdateHandler, c, map[string]any{"client": "192.168.1.50", "comment": "laptop"})

	req := rec.Only(t, "PUT", "/clients/192.168.1.50")
	req.AssertBodyKeys(t, "comment")
	req.AssertNoField(t, "enabled")
	rec.AssertNone(t, "GET", "/clients/192.168.1.50")
}

// Same as the write paths: a client identifier carrying a reserved character
// must reach the row it names, or the tool describes a different client.
func TestClientsList_EscapesTheClientFilter(t *testing.T) {
	const client = "eth0#lan"
	rec := piholeHandler(map[string]any{
		"/clients/" + client: map[string]any{"clients": []any{
			map[string]any{"client": client, "comment": "wired"},
		}},
	})
	c := newTestClient(t, rec)

	callTool(t, clientsListHandler, c, map[string]any{"client": client})

	rec.Only(t, "GET", "/clients/"+client).AssertRawPath(t, "/clients/eth0%23lan")
}

// A client row's only mutable field is its comment, and FTL replaces it on
// every PUT: an update that does not name one erases it. Verified against FTL
// v6.7, where PUT with an empty body left the comment null. The three sibling
// families read the entry back before writing; clients has to as well.
func TestClientsUpdate_OmittedCommentIsPreserved(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /clients/192.168.1.50": map[string]any{"clients": []any{
			map[string]any{"client": "192.168.1.50", "comment": "kids tablet"},
		}},
		"PUT /clients/192.168.1.50": map[string]any{"clients": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, clientsUpdateHandler, c, map[string]any{"client": "192.168.1.50"})

	rec.Only(t, "PUT", "/clients/192.168.1.50").AssertField(t, "comment", "kids tablet")
}

// A caller that supplied the comment replaces it, so there is nothing to carry
// over and no round trip to pay for.
func TestClientsUpdate_SuppliedCommentSkipsTheRead(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"PUT /clients/192.168.1.50": map[string]any{"clients": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, clientsUpdateHandler, c, map[string]any{"client": "192.168.1.50", "comment": "laptop"})

	rec.AssertNone(t, "GET", "/clients/192.168.1.50")
	rec.Only(t, "PUT", "/clients/192.168.1.50").AssertField(t, "comment", "laptop")
}

func TestClientsUpdate_ReadFailureLeavesTheClientAlone(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"PUT /clients/192.168.1.50": map[string]any{"clients": []any{}},
	})
	c := newTestClient(t, rec)

	callToolExpectError(t, clientsUpdateHandler, c, map[string]any{"client": "192.168.1.50"})
	rec.AssertNone(t, "PUT", "/clients/192.168.1.50")
}

// TestClientsAdd_MissingClientIsNamedError pins requireStrings' wiring at the
// handler level: a caller who forgot 'client' gets told that, and nothing
// reaches the fake.
func TestClientsAdd_MissingClientIsNamedError(t *testing.T) {
	rec := piholeHandler(map[string]any{})
	c := newTestClient(t, rec)

	msg := callToolExpectError(t, clientsAddHandler, c, map[string]any{})
	if !strings.Contains(msg, "'client'") {
		t.Errorf("error %q does not name the missing parameter", msg)
	}
	rec.AssertNone(t, "", "")
}
