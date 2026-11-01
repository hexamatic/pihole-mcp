package server

import (
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Protocol revision 2026-07-28 removes the handshake. A modern client opens no
// session; every request carries the protocol version, client identity and
// capabilities in _meta, and server/discover replaces the handshake's
// capability exchange. mcp-go serves both eras from the same server, so every
// guarantee the legacy tests make has to hold for a modern request as well.
//
// The one that matters most is scoping. PIHOLE_READ_ONLY and PIHOLE_TOOLSETS
// rest on server.WithToolFilter being applied at tools/call, not only at
// tools/list. These tests prove that still holds when the request arrives with
// no session behind it.

// modernMeta is the _meta block a 2026-07-28 client attaches to every request.
var modernMeta = fmt.Sprintf(`{"%s":%q,"%s":{"name":"modern-test","version":"1"},"%s":{}}`,
	mcp.MetaKeyProtocolVersion, mcp.ProtocolVersion20260728,
	mcp.MetaKeyClientInfo, mcp.MetaKeyClientCapabilities)

// modernRequest builds a 2026-07-28 request. params is the request's own
// parameters as a JSON object body without braces, or empty.
func modernRequest(method, params string) string {
	if params != "" {
		params += ","
	}
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":{%s"_meta":%s}}`, method, params, modernMeta)
}

func modernListedTools(t *testing.T, opts []Option) map[string]bool {
	t.Helper()
	srv := newScopedDispatchServer(t, opts, newDispatchFake(t))
	res := dispatchResult(t, dispatch(t, srv, modernRequest("tools/list", "")))
	raw, ok := res["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list result has no tools array: %#v", res)
	}
	out := make(map[string]bool, len(raw))
	for _, entry := range raw {
		tool, _ := entry.(map[string]any)
		name, _ := tool["name"].(string)
		ann, _ := tool["annotations"].(map[string]any)
		readOnly, _ := ann["readOnlyHint"].(bool)
		out[name] = readOnly
	}
	return out
}

// TestModernDiscoverAdvertisesTheRevision checks a modern client can find out
// what this server speaks without the legacy handshake, and that it is told the
// same instructions a legacy client receives from initialize.
//
//nolint:misspell // "initialize" is an MCP protocol method name
func TestModernDiscoverAdvertisesTheRevision(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readOnly=%v", readOnly), func(t *testing.T) {
			srv := newScopedDispatchServer(t, []Option{WithReadOnly(readOnly)}, newDispatchFake(t))

			res := dispatchResult(t, dispatch(t, srv, modernRequest(string(mcp.MethodServerDiscover), "")))

			versions, _ := res["supportedVersions"].([]any)
			found := false
			for _, v := range versions {
				if v == mcp.ProtocolVersion20260728 {
					found = true
				}
			}
			if !found {
				t.Errorf("server/discover supportedVersions = %v, want it to include %s", versions, mcp.ProtocolVersion20260728)
			}

			if got, want := res["instructions"], instructions(readOnly); got != want {
				t.Errorf("server/discover instructions differ from the legacy handshake's:\n got %q\nwant %q", got, want)
			}

			legacy := dispatchResult(t, dispatch(t, srv, `{"jsonrpc":"2.0","id":2,"method":"initialize",
				"params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy","version":"1"}}}`))
			if legacy["instructions"] != res["instructions"] {
				t.Error("a modern and a legacy client of the same server receive different instructions")
			}
		})
	}
}

// TestModernReadOnlyListsOnlyReadOnlyTools is the visibility half of read-only
// mode for a sessionless, modern request.
func TestModernReadOnlyListsOnlyReadOnlyTools(t *testing.T) {
	listed := modernListedTools(t, []Option{WithReadOnly(true)})
	if len(listed) == 0 {
		t.Fatal("a modern tools/list in read-only mode returned no tools")
	}
	for name, readOnly := range listed {
		if !readOnly {
			t.Errorf("read-only mode lists %s to a modern client, which is not read-only", name)
		}
	}
}

// TestModernReadOnlyRejectsWriteToolCall is the access-control half: a modern
// client that names a write tool directly, with no session and no prior
// tools/list, is refused exactly as a legacy one is.
func TestModernReadOnlyRejectsWriteToolCall(t *testing.T) {
	srv := newScopedDispatchServer(t, []Option{WithReadOnly(true)}, newDispatchFake(t))

	envelope := dispatch(t, srv, modernRequest("tools/call",
		`"name":"pihole_domains_add","arguments":{"domain":"example.com","type":"deny","kind":"exact"}`))

	if _, present := envelope["error"]; !present {
		t.Fatalf("a modern client called a write tool in read-only mode and it ran: %#v", envelope["result"])
	}
}

// TestModernReadOnlyStillServesReadTools guards against the opposite failure:
// a filter that refuses everything for modern requests would pass the test
// above while breaking every modern client.
func TestModernReadOnlyStillServesReadTools(t *testing.T) {
	srv := newScopedDispatchServer(t, []Option{WithReadOnly(true)}, newDispatchFake(t))

	res := dispatchResult(t, dispatch(t, srv, modernRequest("tools/call",
		`"name":"pihole_domains_list","arguments":{}`)))

	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("a read tool failed for a modern client in read-only mode: %#v", res)
	}
}

// TestModernToolsetsScopeListAndCall applies the same two checks to
// PIHOLE_TOOLSETS. The listing a modern client sees must be exactly the one a
// legacy client sees for the same selection, and a tool outside it must not be
// callable.
func TestModernToolsetsScopeListAndCall(t *testing.T) {
	opts := []Option{WithToolsets([]string{"dns"})}

	modern := modernListedTools(t, opts)
	legacy := listedTools(t, newScopedDispatchServer(t, opts, newDispatchFake(t)))
	if len(modern) == 0 || len(modern) == len(listedTools(t, newScopedDispatchServer(t, nil, newDispatchFake(t)))) {
		t.Fatalf("toolsets=dns did not narrow the modern listing (%d tools)", len(modern))
	}
	for name := range legacy {
		if _, ok := modern[name]; !ok {
			t.Errorf("toolsets=dns lists %s to a legacy client but not to a modern one", name)
		}
	}
	for name := range modern {
		if _, ok := legacy[name]; !ok {
			t.Errorf("toolsets=dns lists %s to a modern client but not to a legacy one", name)
		}
	}
	if _, ok := modern["pihole_domains_list"]; ok {
		t.Fatal("toolsets=dns lists pihole_domains_list; the call check below would prove nothing")
	}

	srv := newScopedDispatchServer(t, opts, newDispatchFake(t))
	envelope := dispatch(t, srv, modernRequest("tools/call",
		`"name":"pihole_domains_list","arguments":{}`))
	if _, present := envelope["error"]; !present {
		t.Fatalf("a modern client called a tool outside toolsets=dns and it ran: %#v", envelope["result"])
	}
}
