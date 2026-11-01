package tools

import (
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/server"
)

// listHostsHandler adapts the shared list handler to the shape callTool takes.
func listHostsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return localRecordsListHandler(r, hostsField, "local DNS")
}

// nestedDNS builds the config body FTL actually returns. The nesting is the
// point: a request for the dns section arrives wrapped in one object per path
// component from the root, and a handler reading the field off the wrapper sees
// an empty list on a Pi-hole that is full of records.
func nestedDNS(hosts, cnames []any) map[string]any {
	return map[string]any{
		"config": map[string]any{
			"dns": map[string]any{
				"hosts":        hosts,
				"cnameRecords": cnames,
			},
		},
	}
}

func TestLocalDNSList_ReadsTheNestedShape(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns": nestedDNS([]any{"192.168.1.50 nas.home", "10.0.0.5 pi.home"}, nil),
	})
	c := newTestClient(t, rec)

	text := callTool(t, listHostsHandler, c, map[string]any{})
	for _, want := range []string{"192.168.1.50 nas.home", "10.0.0.5 pi.home", "2 local DNS record"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in output, got: %s", want, text)
		}
	}
}

func TestLocalDNSList_Empty(t *testing.T) {
	rec := piholeHandler(map[string]any{"/config/dns": nestedDNS(nil, nil)})
	c := newTestClient(t, rec)

	text := callTool(t, listHostsHandler, c, map[string]any{})
	if !strings.Contains(text, "No local DNS records configured") {
		t.Errorf("unexpected output: %s", text)
	}
}

// A host record is "<ip> <hostname>", so the value put in the path always
// contains a space. This is the case the old concatenation mishandled, and no
// tool-level test covered it before.
func TestLocalDNSAdd_EscapesTheSpaceInTheRecord(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns/hosts/192.168.1.50 nas.home": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, localDNSAddHandler, c, map[string]any{
		"ip": "192.168.1.50", "hostname": "nas.home",
	})
	if !strings.Contains(text, "Added") || !strings.Contains(text, "A record") {
		t.Errorf("expected an A record confirmation, got: %s", text)
	}

	req := rec.Only(t, "PUT", "/config/dns/hosts/192.168.1.50 nas.home")
	req.AssertRawPath(t, "/config/dns/hosts/192.168.1.50%20nas.home")
	req.AssertNoQueryString(t)
	// The value lives entirely in the path. A body here would be a second,
	// contradictory statement of what to add.
	req.AssertNoBody(t)
}

func TestLocalDNSAdd_IPv6IsAnAAAARecord(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns/hosts/fd00::1 nas.home": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, localDNSAddHandler, c, map[string]any{
		"ip": "fd00::1", "hostname": "nas.home",
	})
	if !strings.Contains(text, "AAAA record") {
		t.Errorf("expected an AAAA record confirmation, got: %s", text)
	}
	rec.Only(t, "PUT", "/config/dns/hosts/fd00::1 nas.home").
		AssertRawPath(t, "/config/dns/hosts/fd00::1%20nas.home")
}

func TestLocalDNSAdd_RestartFalseSurvivesTheEscaping(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns/hosts/192.168.1.50 nas.home": map[string]any{},
	})
	c := newTestClient(t, rec)

	callTool(t, localDNSAddHandler, c, map[string]any{
		"ip": "192.168.1.50", "hostname": "nas.home", "restart": false,
	})
	rec.Only(t, "PUT", "/config/dns/hosts/192.168.1.50 nas.home").
		AssertQuery(t, "restart", "false")
}

func TestLocalDNSAdd_RejectsABadAddress(t *testing.T) {
	c := newTestClient(t, piholeHandler(nil))

	text := callToolExpectError(t, localDNSAddHandler, c, map[string]any{
		"ip": "not-an-ip", "hostname": "nas.home",
	})
	if !strings.Contains(text, "not an IP address") {
		t.Errorf("expected an actionable address error, got: %s", text)
	}
}

// The delete resolves against the stored list rather than reconstructing the
// value. A record may carry several names for one address, and a DELETE of the
// reconstruction would 404 while the record stayed put.
func TestLocalDNSDelete_KeepsTheOtherNamesOnTheLine(t *testing.T) {
	// FTL deletes an array item only as a whole, and a line migrated from a
	// Pi-hole v5 custom.list can carry several names. Deleting one used to
	// delete them all.
	rec := piholeHandler(map[string]any{
		"/config/dns":                                        nestedDNS([]any{"192.168.1.50 nas nas.home", "10.0.0.5 pi.home"}, nil),
		"PUT /config/dns/hosts/192.168.1.50 nas":             map[string]any{},
		"DELETE /config/dns/hosts/192.168.1.50 nas nas.home": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, localDNSDeleteHandler, c, map[string]any{
		"ip": "192.168.1.50", "hostname": "nas.home",
	})
	if !strings.Contains(text, "192.168.1.50 nas`") {
		t.Errorf("expected the reply to name the record that remains, got: %s", text)
	}

	reqs := rec.Requests()
	var writes []string
	for _, r := range reqs {
		if r.Method != "GET" {
			writes = append(writes, r.Method+" "+r.RawPath+"?"+r.RawQuery)
		}
	}
	want := []string{
		"PUT /config/dns/hosts/192.168.1.50%20nas?restart=false",
		"DELETE /config/dns/hosts/192.168.1.50%20nas%20nas.home?",
	}
	if strings.Join(writes, "\n") != strings.Join(want, "\n") {
		t.Errorf("writes = %q, want the reduced line added before the original is deleted: %q", writes, want)
	}
}

func TestLocalDNSDelete_DeletesASingleNameLine(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns": nestedDNS([]any{"10.0.0.5 pi.home"}, nil),
		"DELETE /config/dns/hosts/10.0.0.5 pi.home": map[string]any{},
	})
	c := newTestClient(t, rec)

	callTool(t, localDNSDeleteHandler, c, map[string]any{"ip": "10.0.0.5", "hostname": "pi.home"})

	req := rec.Only(t, "DELETE", "/config/dns/hosts/10.0.0.5 pi.home")
	req.AssertRawPath(t, "/config/dns/hosts/10.0.0.5%20pi.home")
	req.AssertNoBody(t)
	rec.AssertNone(t, "PUT", "")
}

func TestLocalDNSDelete_MatchesAnIPv6AddressHoweverItIsWritten(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns": nestedDNS([]any{"fd00:0:0::5 nas.home"}, nil),
		"DELETE /config/dns/hosts/fd00:0:0::5 nas.home": map[string]any{},
	})
	c := newTestClient(t, rec)

	callTool(t, localDNSDeleteHandler, c, map[string]any{"ip": "fd00::5", "hostname": "nas.home"})
	rec.Only(t, "DELETE", "/config/dns/hosts/fd00:0:0::5 nas.home")
}

func TestLocalCNAMEDelete_KeepsTheOtherAliases(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns": nestedDNS(nil, []any{"a.home,b.home,nas.home,300"}),
		"PUT /config/dns/cnameRecords/b.home,nas.home,300":           map[string]any{},
		"DELETE /config/dns/cnameRecords/a.home,b.home,nas.home,300": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, localCNAMEDeleteHandler, c, map[string]any{"alias": "a.home"})
	if !strings.Contains(text, "b.home,nas.home,300") {
		t.Errorf("expected the remaining alias to be kept, got: %s", text)
	}
	rec.Only(t, "PUT", "/config/dns/cnameRecords/b.home,nas.home,300")
	rec.Only(t, "DELETE", "/config/dns/cnameRecords/a.home,b.home,nas.home,300")
}

func TestLocalDNSDelete_NoMatchDeletesNothing(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns": nestedDNS([]any{"10.0.0.5 pi.home"}, nil),
	})
	c := newTestClient(t, rec)

	text := callToolExpectError(t, localDNSDeleteHandler, c, map[string]any{
		"ip": "192.168.1.50", "hostname": "nas.home",
	})
	if !strings.Contains(text, "nothing was deleted") {
		t.Errorf("expected an explicit no-op message, got: %s", text)
	}
	// The important half: no DELETE was attempted at all.
	rec.AssertNone(t, "DELETE", "")
}

func TestLocalCNAMEAdd_WithAndWithoutTTL(t *testing.T) {
	t.Run("without ttl", func(t *testing.T) {
		rec := piholeHandler(map[string]any{
			"/config/dns/cnameRecords/files.home,nas.home": map[string]any{},
		})
		c := newTestClient(t, rec)

		callTool(t, localCNAMEAddHandler, c, map[string]any{
			"alias": "files.home", "target": "nas.home",
		})
		rec.Only(t, "PUT", "/config/dns/cnameRecords/files.home,nas.home").
			AssertRawPath(t, "/config/dns/cnameRecords/files.home%2Cnas.home")
	})

	t.Run("with ttl", func(t *testing.T) {
		rec := piholeHandler(map[string]any{
			"/config/dns/cnameRecords/files.home,nas.home,300": map[string]any{},
		})
		c := newTestClient(t, rec)

		callTool(t, localCNAMEAddHandler, c, map[string]any{
			"alias": "files.home", "target": "nas.home", "ttl": 300,
		})
		rec.Only(t, "PUT", "/config/dns/cnameRecords/files.home,nas.home,300")
	})
}

func TestLocalCNAMEDelete_MatchesOnAliasAlone(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns": nestedDNS(nil, []any{"files.home,nas.home,300", "www.home,nas.home"}),
		"DELETE /config/dns/cnameRecords/files.home,nas.home,300": map[string]any{},
	})
	c := newTestClient(t, rec)

	// The caller knows the alias, not the TTL the record was stored with.
	text := callTool(t, localCNAMEDeleteHandler, c, map[string]any{"alias": "files.home"})
	if !strings.Contains(text, "files.home,nas.home,300") {
		t.Errorf("expected the stored record in the confirmation, got: %s", text)
	}
	rec.Only(t, "DELETE", "/config/dns/cnameRecords/files.home,nas.home,300")
}
