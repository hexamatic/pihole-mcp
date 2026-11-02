package tools

import (
	"testing"

	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/server"
)

// fixtureValueCase runs one read tool against every version's capture of
// the endpoint it reads, and checks that values from the capture appear in
// what the tool rendered.
//
// These tests used to check only that a tool returned something. A decoder
// reading the wrong field still returns something, so changing the JSON tags
// on the decoded types went unnoticed by all but one of them.
type fixtureValueCase struct {
	fixture string
	route   string
	handler func(*pihole.Registry) server.ToolHandlerFunc
	args    map[string]any
	// want lists the strings the output must contain, derived from the
	// capture. An empty list means the capture has nothing to check, which
	// fails the test so that a reseeded capture is noticed.
	want func(t *testing.T, fixture any) []string
}

// first returns the first element of the array at dotted in fixture, or nil
// when the array is empty.
func first(t *testing.T, fixture any, dotted string) map[string]any {
	t.Helper()
	list, ok := fixtureValue(t, fixture, dotted).([]any)
	if !ok {
		t.Fatalf("fixture %s is not an array", dotted)
	}
	if len(list) == 0 {
		return nil
	}
	m, _ := list[0].(map[string]any)
	return m
}

// nonZeroTypes is the "TYPE: count" pairs of the query types with a count,
// as the query type tools render them.
func nonZeroTypes(t *testing.T, f any) []string {
	t.Helper()
	var out []string
	for name, v := range fixtureValue(t, f, "types").(map[string]any) {
		if n, _ := v.(float64); n > 0 {
			out = append(out, name+": "+number(v))
		}
	}
	return out
}

func number(v any) string {
	f, _ := v.(float64)
	return format.Number(int(f))
}

func TestReadTools_RenderTheValuesPiholeSent(t *testing.T) {
	for _, tc := range []fixtureValueCase{
		{
			fixture: "stats_summary", route: "/stats/summary", handler: statsSummaryHandler,
			want: func(t *testing.T, f any) []string {
				return []string{
					format.Number(fixtureInt(t, f, "queries.total")),
					format.Number(fixtureInt(t, f, "queries.blocked")),
					format.Number(fixtureInt(t, f, "gravity.domains_being_blocked")),
				}
			},
		},
		{
			fixture: "stats_top_domains", route: "/stats/top_domains", handler: statsTopDomainsHandler,
			want: func(t *testing.T, f any) []string {
				d := first(t, f, "domains")
				if d == nil {
					return nil
				}
				return []string{d["domain"].(string), number(d["count"])}
			},
		},
		{
			fixture: "stats_top_clients", route: "/stats/top_clients", handler: statsTopClientsHandler,
			want: func(t *testing.T, f any) []string {
				cl := first(t, f, "clients")
				if cl == nil {
					return nil
				}
				return []string{cl["ip"].(string), number(cl["count"])}
			},
		},
		{
			fixture: "stats_database_top_clients", route: "/stats/database/top_clients", handler: statsDatabaseTopClientsHandler,
			want: func(t *testing.T, f any) []string {
				cl := first(t, f, "clients")
				if cl == nil {
					return nil
				}
				return []string{cl["ip"].(string), number(cl["count"])}
			},
		},
		{
			fixture: "stats_query_types", route: "/stats/query_types", handler: statsQueryTypesHandler,
			want: nonZeroTypes,
		},
		{
			fixture: "stats_database_query_types", route: "/stats/database/query_types", handler: statsDatabaseQueryTypesHandler,
			want: nonZeroTypes,
		},
		{
			fixture: "stats_recent_blocked", route: "/stats/recent_blocked", handler: statsRecentBlockedHandler,
			want: func(t *testing.T, f any) []string {
				list := fixtureValue(t, f, "blocked").([]any)
				if len(list) == 0 {
					return nil
				}
				return []string{list[0].(string)}
			},
		},
		{
			fixture: "info_database", route: "/info/database", handler: infoDatabaseHandler,
			want: func(t *testing.T, f any) []string {
				return []string{format.Number(fixtureInt(t, f, "queries")), fixtureString(t, f, "sqlite_version")}
			},
		},
		{
			fixture: "info_messages", route: "/info/messages", handler: infoMessagesHandler,
			want: func(t *testing.T, f any) []string {
				m := first(t, f, "messages")
				if m == nil {
					return nil
				}
				return []string{m["plain"].(string), "id=" + number(m["id"])}
			},
		},
		{
			fixture: "info_version", route: "/info/version", handler: infoVersionHandler,
			want: func(t *testing.T, f any) []string {
				return []string{
					fixtureString(t, f, "version.core.local.version"),
					fixtureString(t, f, "version.ftl.local.version"),
					fixtureString(t, f, "version.web.local.version"),
					fixtureString(t, f, "version.docker.local"),
				}
			},
		},
		{
			fixture: "network_devices", route: "/network/devices", handler: networkDevicesHandler,
			want: func(t *testing.T, f any) []string {
				d := first(t, f, "devices")
				if d == nil {
					return nil
				}
				return []string{d["hwaddr"].(string)}
			},
		},
		{
			fixture: "network_interfaces", route: "/network/interfaces", handler: networkInterfacesHandler,
			want: func(t *testing.T, f any) []string {
				i := first(t, f, "interfaces")
				if i == nil {
					return nil
				}
				return []string{i["name"].(string)}
			},
		},
		{
			fixture: "network_routes", route: "/network/routes", handler: networkRoutesHandler,
			want: func(t *testing.T, f any) []string {
				r := first(t, f, "routes")
				if r == nil {
					return nil
				}
				return []string{r["gateway"].(string), r["oif"].(string)}
			},
		},
		{
			fixture: "auth_sessions", route: "/auth/sessions", handler: authSessionsHandler,
			want: func(t *testing.T, f any) []string {
				s := first(t, f, "sessions")
				if s == nil {
					return nil
				}
				return []string{s["remote_addr"].(string), s["user_agent"].(string)}
			},
		},
		{
			fixture: "config_properties", route: "/config/_properties", handler: configPropertiesHandler,
			want: func(t *testing.T, f any) []string {
				p := first(t, f, "config.read_only")
				if p == nil {
					return nil
				}
				return []string{p["key"].(string)}
			},
		},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			eachFixture(t, tc.fixture, func(t *testing.T, _ string, fixture any) {
				want := tc.want(t, fixture)
				if len(want) == 0 {
					t.Skip("this capture has nothing to check")
				}
				c := newTestClient(t, piholeHandler(map[string]any{tc.route: fixture}))
				text := callTool(t, tc.handler, c, tc.args)
				for _, w := range want {
					if !strings.Contains(text, w) {
						t.Errorf("expected %q from the capture in the output, got:\n%s", w, text)
					}
				}
			})
		})
	}
}

func TestVersionAndDashboard_WarnOfPublishedAdvisories(t *testing.T) {
	// The v6.7 captures come from a release with published advisories that
	// v6.8 fixes; the v6.8 captures from one with none.
	for _, tc := range []struct {
		fixture, route string
		handler        func(*pihole.Registry) server.ToolHandlerFunc
	}{
		{"info_version", "/info/version", infoVersionHandler},
		{"padd", "/padd", paddHandler},
	} {
		eachFixture(t, tc.fixture, func(t *testing.T, version string, fixture any) {
			text := callTool(t, tc.handler, newTestClient(t, piholeHandler(map[string]any{tc.route: fixture})), nil)
			warned := strings.Contains(text, "**Security:** FTL v6.7 is affected by")
			if want := version == "v6.7"; warned != want {
				t.Errorf("%s on %s: warned=%v, want %v:\n%s", tc.fixture, version, warned, want, text)
			}
		})
	}
}
