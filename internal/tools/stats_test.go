package tools

import (
	"strings"
	"testing"
)

func TestStatsSummary_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/summary": map[string]any{
			"queries": map[string]any{
				"total": 45231, "blocked": 12847, "percent_blocked": 28.4,
				"cached": 18492, "forwarded": 13892, "unique_domains": 445,
				"frequency": 1.1, "types": map[string]any{"A": 30000, "AAAA": 10000},
				"status": map[string]any{}, "replies": map[string]any{},
			},
			"clients": map[string]any{"active": 23, "total": 30},
			"gravity": map[string]any{"domains_being_blocked": 92277, "last_update": 1712345678},
		},
	}))

	text := callTool(t, statsSummaryHandler, c, nil)
	if !strings.Contains(text, "45,231") {
		t.Errorf("expected formatted query count, got: %s", text)
	}
	if !strings.Contains(text, "28.4%") {
		t.Errorf("expected blocking percentage, got: %s", text)
	}
	if !strings.Contains(text, "92,277") {
		t.Errorf("expected gravity count, got: %s", text)
	}
}

func TestStatsSummary_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/summary": map[string]any{
			"queries": map[string]any{
				"total": 100, "blocked": 20, "percent_blocked": 20.0,
				"cached": 50, "forwarded": 30, "unique_domains": 10,
				"frequency": 0.5, "types": map[string]any{}, "status": map[string]any{}, "replies": map[string]any{},
			},
			"clients": map[string]any{"active": 5, "total": 5},
			"gravity": map[string]any{"domains_being_blocked": 1000, "last_update": 0},
		},
	}))

	text := callTool(t, statsSummaryHandler, c, map[string]any{"detail": "minimal"})
	// Minimal should be a single line.
	if strings.Count(text, "\n") > 1 {
		t.Errorf("minimal should be single-line, got: %s", text)
	}
	if !strings.Contains(text, "100") {
		t.Errorf("expected query count in minimal, got: %s", text)
	}
}

func TestStatsSummary_Full(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/summary": map[string]any{
			"queries": map[string]any{
				"total": 100, "blocked": 20, "percent_blocked": 20.0,
				"cached": 50, "forwarded": 30, "unique_domains": 10,
				"frequency": 0.5,
				"types":     map[string]any{"A": 80, "AAAA": 20},
				"status":    map[string]any{"FORWARDED": 30, "CACHE": 50, "GRAVITY": 20},
				"replies":   map[string]any{},
			},
			"clients": map[string]any{"active": 5, "total": 5},
			"gravity": map[string]any{"domains_being_blocked": 1000, "last_update": 0},
		},
	}))

	text := callTool(t, statsSummaryHandler, c, map[string]any{"detail": "full"})
	if !strings.Contains(text, "Status breakdown") {
		t.Errorf("full should include status breakdown, got: %s", text)
	}
	if !strings.Contains(text, "Unique domains") {
		t.Errorf("full should include unique domains, got: %s", text)
	}
}

func TestStatsTopDomains_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/top_domains": map[string]any{
			"domains":         []any{map[string]any{"domain": "example.com", "count": 100}},
			"total_queries":   1000,
			"blocked_queries": 200,
		},
	}))

	text := callTool(t, statsTopDomainsHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Rank,Domain,Queries") {
		t.Errorf("CSV should have header row, got: %s", text)
	}
	if !strings.Contains(text, "example.com") {
		t.Errorf("CSV should contain domain, got: %s", text)
	}
}

func TestStatsDatabaseTopDomains_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/database/top_domains": map[string]any{
			"domains":         []any{map[string]any{"domain": "google.com", "count": 500}, map[string]any{"domain": "facebook.com", "count": 300}},
			"total_queries":   5000,
			"blocked_queries": 1000,
		},
	}))

	text := callTool(t, statsDatabaseTopDomainsHandler, c, nil)
	if !strings.Contains(text, "google.com") {
		t.Errorf("expected domain name, got: %s", text)
	}
	if !strings.Contains(text, "500") {
		t.Errorf("expected domain count, got: %s", text)
	}
	if !strings.Contains(text, "facebook.com") {
		t.Errorf("expected second domain, got: %s", text)
	}
	if !strings.Contains(text, "300") {
		t.Errorf("expected second count, got: %s", text)
	}
}

func TestStatsDatabaseTopClients_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/database/top_clients": map[string]any{
			"clients":         []any{map[string]any{"ip": "192.168.1.10", "name": "desktop", "count": 1200}, map[string]any{"ip": "192.168.1.20", "name": "", "count": 800}},
			"total_queries":   5000,
			"blocked_queries": 1000,
		},
	}))

	text := callTool(t, statsDatabaseTopClientsHandler, c, nil)
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected client IP, got: %s", text)
	}
	if !strings.Contains(text, "desktop") {
		t.Errorf("expected client name, got: %s", text)
	}
	if !strings.Contains(text, "1,200") {
		t.Errorf("expected formatted count, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.20") {
		t.Errorf("expected second client IP, got: %s", text)
	}
}

func TestStatsDatabaseUpstreams_Normal(t *testing.T) {
	dns1 := "8.8.8.8"
	name1 := "google-dns"
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/database/upstreams": map[string]any{
			"upstreams": []any{
				map[string]any{"ip": &dns1, "name": &name1, "port": 53, "count": 3000, "statistics": map[string]any{"response": 12.5, "variance": 2.1}},
			},
			"forwarded_queries": 3000,
			"total_queries":     5000,
		},
	}))

	text := callTool(t, statsDatabaseUpstreamsHandler, c, nil)
	if !strings.Contains(text, "google-dns") {
		t.Errorf("expected upstream name, got: %s", text)
	}
	if !strings.Contains(text, "3,000") {
		t.Errorf("expected query count, got: %s", text)
	}
	if !strings.Contains(text, "5,000") {
		t.Errorf("expected total queries, got: %s", text)
	}
}

func TestStatsDatabaseQueryTypes_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/database/query_types": map[string]any{
			"types": map[string]any{"A": 4000, "AAAA": 800, "MX": 50},
		},
	}))

	text := callTool(t, statsDatabaseQueryTypesHandler, c, nil)
	if !strings.Contains(text, "A:") {
		t.Errorf("expected query type A, got: %s", text)
	}
	if !strings.Contains(text, "AAAA") {
		t.Errorf("expected query type AAAA, got: %s", text)
	}
	if !strings.Contains(text, "4,000") {
		t.Errorf("expected A count, got: %s", text)
	}
	if !strings.Contains(text, "MX") {
		t.Errorf("expected query type MX, got: %s", text)
	}
}

func TestStatsUpstreams_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/upstreams": map[string]any{
			"upstreams": []any{
				map[string]any{"name": "Cloudflare", "ip": "1.1.1.1", "port": 53, "count": 1500,
					"statistics": map[string]any{"response": 12.5}},
			},
			"forwarded_queries": 1500,
			"total_queries":     5000,
		},
	}))

	text := callTool(t, statsUpstreamsHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Name,IP,Port,Queries,AvgResponseMs") {
		t.Errorf("CSV should have header row, got: %s", text)
	}
	if !strings.Contains(text, "Cloudflare,1.1.1.1,53,") {
		t.Errorf("CSV should contain upstream row, got: %s", text)
	}
}

func TestStatsQueryTypes_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/query_types": map[string]any{
			"types": map[string]any{"A": 4000, "AAAA": 800},
		},
	}))

	text := callTool(t, statsQueryTypesHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Type,Count") {
		t.Errorf("CSV should have header row, got: %s", text)
	}
	if !strings.Contains(text, "A,") {
		t.Errorf("CSV should contain type A, got: %s", text)
	}
}

func TestStatsRecentBlocked_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/recent_blocked": map[string]any{
			"blocked": []any{"ads.example.com", "tracker.com"},
		},
	}))

	text := callTool(t, statsRecentBlockedHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Rank,Domain") {
		t.Errorf("CSV should have header row, got: %s", text)
	}
	if !strings.Contains(text, "1,ads.example.com") {
		t.Errorf("CSV should contain ranked rows, got: %s", text)
	}
}

func TestStatsDatabaseUpstreams_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/database/upstreams": map[string]any{
			"upstreams": []any{
				map[string]any{"name": "Quad9", "ip": "9.9.9.9", "port": 53, "count": 2000,
					"statistics": map[string]any{"response": 18.0}},
			},
			"forwarded_queries": 2000,
			"total_queries":     6000,
		},
	}))

	text := callTool(t, statsDatabaseUpstreamsHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Name,IP,Port,Queries,AvgResponseMs") {
		t.Errorf("CSV should have header row, got: %s", text)
	}
	if !strings.Contains(text, "Quad9,9.9.9.9,53,") {
		t.Errorf("CSV should contain upstream row, got: %s", text)
	}
}

// Real-fixture tests below — protect against Pi-hole API shape drift by
// running the handler against a captured response from the live dev
// instance. Hand-written mocks above continue to cover specific value
// assertions and edge cases the live fixture can't synthesise.

func TestStatsSummary_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/summary": loadFixture(t, "stats_summary"),
	}))
	text := callTool(t, statsSummaryHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty stats summary from real fixture")
	}
}

func TestStatsTopDomains_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/top_domains": loadFixture(t, "stats_top_domains"),
	}))
	text := callTool(t, statsTopDomainsHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty top_domains output from real fixture")
	}
}

func TestStatsTopClients_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/top_clients": loadFixture(t, "stats_top_clients"),
	}))
	text := callTool(t, statsTopClientsHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty top_clients output from real fixture")
	}
}

func TestStatsUpstreams_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/upstreams": loadFixture(t, "stats_upstreams"),
	}))
	text := callTool(t, statsUpstreamsHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty upstreams output from real fixture")
	}
}

func TestStatsQueryTypes_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/query_types": loadFixture(t, "stats_query_types"),
	}))
	text := callTool(t, statsQueryTypesHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty query_types output from real fixture")
	}
}

func TestStatsRecentBlocked_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/recent_blocked": loadFixture(t, "stats_recent_blocked"),
	}))
	text := callTool(t, statsRecentBlockedHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty recent_blocked output from real fixture")
	}
}

func TestStatsDatabaseTopDomains_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/database/top_domains": loadFixture(t, "stats_database_top_domains"),
	}))
	text := callTool(t, statsDatabaseTopDomainsHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty database top_domains output from real fixture")
	}
}

func TestStatsDatabaseUpstreams_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/database/upstreams": loadFixture(t, "stats_database_upstreams"),
	}))
	text := callTool(t, statsDatabaseUpstreamsHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty database upstreams output from real fixture")
	}
}

func TestStatsDatabaseQueryTypes_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/stats/database/query_types": loadFixture(t, "stats_database_query_types"),
	}))
	text := callTool(t, statsDatabaseQueryTypesHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty database query_types output from real fixture")
	}
}
