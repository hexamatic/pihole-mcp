package tools

import (
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/format"
)

func TestHistoryGraph_InMemory(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history": map[string]any{
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "total": 100, "cached": 30, "blocked": 20, "forwarded": 50},
				map[string]any{"timestamp": 1700000600.0, "total": 120, "cached": 35, "blocked": 25, "forwarded": 60},
			},
		},
	}))

	text := callTool(t, historyGraphHandler, c, nil)
	if !strings.Contains(text, "2 data points") {
		t.Errorf("expected '2 data points', got: %s", text)
	}
	if !strings.Contains(text, "220") {
		t.Errorf("expected total queries (220), got: %s", text)
	}
}

func TestHistoryGraph_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history": map[string]any{
			"history": []any{},
		},
	}))

	text := callTool(t, historyGraphHandler, c, nil)
	if text != "No history data available." {
		t.Errorf("expected empty history message, got: %s", text)
	}
}

func TestHistoryClients_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": "desktop", "total": 500},
				"192.168.1.20": map[string]any{"name": nil, "total": 200},
			},
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "data": map[string]any{"192.168.1.10": 50, "192.168.1.20": 20}},
			},
		},
	}))

	text := callTool(t, historyClientsHandler, c, nil)
	if !strings.Contains(text, "2 clients") {
		t.Errorf("expected '2 clients', got: %s", text)
	}
	if !strings.Contains(text, "desktop") {
		t.Errorf("expected named client 'desktop', got: %s", text)
	}
	if !strings.Contains(text, "500") {
		t.Errorf("expected query count for desktop, got: %s", text)
	}
}

func TestHistoryClients_NilName(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.30": map[string]any{"name": nil, "total": 75},
			},
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "data": map[string]any{"192.168.1.30": 30}},
			},
		},
	}))

	text := callTool(t, historyClientsHandler, c, nil)
	if !strings.Contains(text, "1 clients") {
		t.Errorf("expected '1 clients', got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.30") {
		t.Errorf("expected client IP, got: %s", text)
	}
	if !strings.Contains(text, "75") {
		t.Errorf("expected query count, got: %s", text)
	}
}

func TestHistoryDatabase_RangeProvided(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/database": map[string]any{
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "total": 100, "cached": 30, "blocked": 20, "forwarded": 50},
				map[string]any{"timestamp": 1700086400.0, "total": 200, "cached": 60, "blocked": 40, "forwarded": 100},
			},
		},
	}))

	text := callTool(t, historyDatabaseHandler, c, map[string]any{
		"from":  1700000000.0,
		"until": 1700172800.0,
	})
	if !strings.Contains(text, "2 data points") {
		t.Errorf("expected '2 data points', got: %s", text)
	}
	if !strings.Contains(text, "300") {
		t.Errorf("expected total queries (300), got: %s", text)
	}
}

func TestHistoryDatabase_DefaultRange(t *testing.T) {
	// Without explicit from/until, the handler should still issue both query
	// params via getTimeRange and return the empty-history message.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/database": map[string]any{
			"history": []any{},
		},
	}))

	text := callTool(t, historyDatabaseHandler, c, nil)
	if text != "No history data available." {
		t.Errorf("expected empty history message, got: %s", text)
	}
}

func TestHistoryDatabaseClients_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/database/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": "desktop", "total": 12345},
				"0.0.0.0":      map[string]any{"name": "other clients", "total": 678},
			},
			"history": []any{
				map[string]any{"timestamp": 1700000000.0, "data": map[string]any{"192.168.1.10": 1234}},
			},
		},
	}))

	text := callTool(t, historyDatabaseClientsHandler, c, map[string]any{
		"from":  1700000000.0,
		"until": 1700604800.0,
	})
	if !strings.Contains(text, "2 clients") {
		t.Errorf("expected '2 clients', got: %s", text)
	}
	if !strings.Contains(text, "desktop") {
		t.Errorf("expected named client 'desktop', got: %s", text)
	}
	if !strings.Contains(text, "other clients") {
		t.Errorf("expected 'other clients' bucket, got: %s", text)
	}
}

// The graph tools exist to return a time series and threw all of it away,
// reporting only how many slots there were. It is ~3.5 KB for a full day, so
// it is opt-in, but it has to be reachable.
func TestHistoryGraph_FullEmitsEverySlot(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history": map[string]any{
			"history": []any{
				map[string]any{"timestamp": 1700000000, "total": 10, "cached": 4, "blocked": 3, "forwarded": 3},
				map[string]any{"timestamp": 1700000600, "total": 20, "cached": 8, "blocked": 6, "forwarded": 6},
			},
		},
	}))

	normal := callTool(t, historyGraphHandler, c, nil)
	if strings.Contains(normal, "Time,Total") {
		t.Errorf("normal detail should stay a summary, got: %s", normal)
	}

	full := callTool(t, historyGraphHandler, c, map[string]any{"detail": "full"})
	if !strings.Contains(full, "Time,Total,Cached,Blocked,Forwarded") {
		t.Errorf("expected per-slot rows at detail=full, got: %s", full)
	}
	if !strings.Contains(full, ",20,8,6,6") {
		t.Errorf("expected the second slot's counts, got: %s", full)
	}

	csv := callTool(t, historyGraphHandler, c, map[string]any{"format": "csv"})
	if !strings.HasPrefix(csv, "Time,Total,Cached,Blocked,Forwarded") {
		t.Errorf("expected bare CSV at format=csv, got: %s", csv)
	}
}

// Ranging over the client map meant two identical calls listed the same
// clients in a different order, which reads as movement in the data.
func TestHistoryClients_SortedByTotal(t *testing.T) {
	routes := map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": "desktop", "total": 5},
				"192.168.1.20": map[string]any{"name": nil, "total": 90},
				"192.168.1.30": map[string]any{"name": nil, "total": 5},
			},
			"history": []any{},
		},
	}

	first := callTool(t, historyClientsHandler, newTestClient(t, piholeHandler(routes)), nil)
	want := "**3 clients:**\n" +
		"- 192.168.1.20 — 90 queries\n" +
		"- desktop (192.168.1.10) — 5 queries\n" +
		"- 192.168.1.30 — 5 queries\n"
	if first != want {
		t.Errorf("got:\n%s\nwant busiest first, ties by client:\n%s", first, want)
	}

	for i := range 8 {
		got := callTool(t, historyClientsHandler, newTestClient(t, piholeHandler(routes)), nil)
		if got != first {
			t.Fatalf("call %d rendered a different order:\n%s", i, got)
		}
	}
}

func TestHistoryClients_FullEmitsTheSeries(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": "desktop", "total": 7},
				"192.168.1.20": map[string]any{"name": nil, "total": 3},
			},
			"history": []any{
				map[string]any{"timestamp": 1700000000, "data": map[string]any{"192.168.1.10": 4, "192.168.1.20": 1}},
				map[string]any{"timestamp": 1700000600, "data": map[string]any{"192.168.1.10": 3, "192.168.1.20": 2}},
			},
		},
	}))

	full := callTool(t, historyClientsHandler, c, map[string]any{"detail": "full"})
	if !strings.HasPrefix(full, "Time,desktop (192.168.1.10),192.168.1.20\n") {
		t.Errorf("expected one column per client, busiest first, got: %s", full)
	}
	if !strings.Contains(full, ",3,2\n") {
		t.Errorf("expected the second slot's per-client counts, got: %s", full)
	}
}

func TestHistoryClients_NegativeCountIsNamedError(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{"clients": map[string]any{}, "history": []any{}},
	}))

	msg := callToolExpectError(t, historyClientsHandler, c, map[string]any{"count": -5})
	if !strings.Contains(msg, "'count'") {
		t.Errorf("error %q does not name the parameter", msg)
	}
}

// detail=full asks for the series. Saying so when there is none beats an
// empty table that reads as "no traffic".
func TestHistoryClients_FullWithNoSeries(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": nil, "total": 7},
			},
			"history": []any{},
		},
	}))

	text := callTool(t, historyClientsHandler, c, map[string]any{"detail": "full"})
	if text != "No per-slot client history returned." {
		t.Errorf("got %q, want a named empty series", text)
	}
}

func TestHistoryClients_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": nil, "total": 7},
				"192.168.1.20": map[string]any{"name": nil, "total": 3},
			},
			"history": []any{},
		},
	}))

	if text := callTool(t, historyClientsHandler, c, map[string]any{"detail": "minimal"}); text != "**2 clients.**\n" {
		t.Errorf("got %q, want a single-line count", text)
	}
}

func TestHistoryDatabase_CSVEmitsSlots(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/database": map[string]any{
			"history": []any{
				map[string]any{"timestamp": 1700000000, "total": 11, "cached": 5, "blocked": 3, "forwarded": 3},
				map[string]any{"timestamp": 1700086400, "total": 22, "cached": 9, "blocked": 7, "forwarded": 6},
			},
		},
	}))

	text := callTool(t, historyDatabaseHandler, c, map[string]any{"format": "csv"})
	if !strings.HasPrefix(text, "Time,Total,Cached,Blocked,Forwarded\n") {
		t.Errorf("expected per-slot CSV, got: %s", text)
	}
	if !strings.Contains(text, ",22,9,7,6") {
		t.Errorf("expected the second bucket's counts, got: %s", text)
	}
}

func TestHistoryClients_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients": map[string]any{
			"clients": map[string]any{
				"192.168.1.10": map[string]any{"name": "desktop", "total": 7},
				"192.168.1.20": map[string]any{"name": nil, "total": 3},
			},
			"history": []any{},
		},
	}))

	text := callTool(t, historyClientsHandler, c, map[string]any{"format": "csv"})
	want := "Client,Name,Queries\n192.168.1.10,desktop,7\n192.168.1.20,,3\n"
	if text != want {
		t.Errorf("got:\n%s\nwant:\n%s", text, want)
	}
}

// The long-term endpoint names its clients by address but keys its per-slot
// buckets by database row id, so those ids are not clients. Deriving clients
// from the series here invented two named "1" and "2" alongside the real
// 127.0.0.1. Verified against FTL v6.7.
func TestHistoryDatabaseClients_DoesNotInventClientsFromBucketIDs(t *testing.T) {
	routes := map[string]any{
		"/history/database/clients": map[string]any{
			"clients": map[string]any{
				"127.0.0.1": map[string]any{"name": ""},
			},
			"history": []any{
				map[string]any{"timestamp": 1788092400, "data": map[string]any{"1": 1, "2": 1}},
			},
		},
	}
	c := newTestClient(t, piholeHandler(routes))

	text := callTool(t, historyDatabaseClientsHandler, c, nil)
	if !strings.Contains(text, "**1 clients:**") {
		t.Errorf("expected only the declared client, got: %s", text)
	}
	for _, invented := range []string{"- 1 —", "- 2 —"} {
		if strings.Contains(text, invented) {
			t.Errorf("invented a client from a bucket id (%q) in: %s", invented, text)
		}
	}
	// The counts must not simply vanish either.
	if !strings.Contains(text, "does not map to a client address (1, 2)") {
		t.Errorf("expected the unattributed-bucket note, got: %s", text)
	}

	full := callTool(t, historyDatabaseClientsHandler, c, map[string]any{"detail": "full"})
	if !strings.Contains(full, "Time,127.0.0.1,bucket 1,bucket 2") {
		t.Errorf("expected buckets under their own keys, got: %s", full)
	}
	if !strings.Contains(full, ",0,1,1") {
		t.Errorf("expected the bucket counts, got: %s", full)
	}
}

func TestHistoryClients_HiddenByPrivacy(t *testing.T) {
	// At privacy level 2 or above FTL keeps no per-client data. v6.7 sends
	// the client list as an empty array, which failed to decode; v6.8 sends
	// an empty object, which read as a network with no clients.
	for name, clients := range map[string]any{"v6.7 array": []any{}, "v6.8 object": map[string]any{}} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, piholeHandler(map[string]any{
				"/history/clients":          map[string]any{"clients": clients, "history": []any{}},
				"/config/misc/privacylevel": map[string]any{"config": map[string]any{"misc": map[string]any{"privacylevel": 2}}},
			}))
			text := callTool(t, historyClientsHandler, c, nil)
			if !strings.Contains(text, "No client history") || !strings.Contains(text, "privacy level (misc.privacylevel) is 2") {
				t.Errorf("expected the empty history to be put down to the privacy level, got: %s", text)
			}
		})
	}
}

func TestHistoryClients_EmptyWithoutPrivacyIsNotBlamedOnIt(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/history/clients":          map[string]any{"clients": map[string]any{}, "history": []any{}},
		"/config/misc/privacylevel": map[string]any{"config": map[string]any{"misc": map[string]any{"privacylevel": 0}}},
	}))
	if text := callTool(t, historyClientsHandler, c, nil); strings.Contains(text, "privacy") {
		t.Errorf("privacy level 0 hides nothing, got: %s", text)
	}
}

func TestHistoryClients_CountZeroAsksForEveryClient(t *testing.T) {
	// FTL reads N=0 as every client; leaving N out applies its default.
	rec := piholeHandler(map[string]any{"/history/clients": loadFixture(t, "history_clients")})
	callTool(t, historyClientsHandler, newTestClient(t, rec), map[string]any{"count": 0.0})
	rec.Only(t, "GET", "/history/clients").AssertQuery(t, "N", "0")
}

func TestHistoryDatabaseClients_RealFixtures(t *testing.T) {
	// v6.8 gives each client a total. v6.7 gives none and keys its buckets
	// by database row, and every client used to be reported with 0 queries.
	eachFixture(t, "history_database_clients", func(t *testing.T, version string, fixture any) {
		c := newTestClient(t, piholeHandler(map[string]any{"/history/database/clients": fixture}))
		text := callTool(t, historyDatabaseClientsHandler, c, nil)
		for key, raw := range fixtureValue(t, fixture, "clients").(map[string]any) {
			total, ok := raw.(map[string]any)["total"].(float64)
			if !ok {
				if strings.Contains(text, " 0 queries") || !strings.Contains(text, "does not report a query count per client") {
					t.Errorf("%s: a client with no total should not be shown with 0 queries, got:\n%s", key, text)
				}
				continue
			}
			if want := key + ") — " + format.Number(int(total)) + " queries"; !strings.Contains(text, want) {
				t.Errorf("%s: expected %q, got:\n%s", version, want, text)
			}
		}
	})
}
