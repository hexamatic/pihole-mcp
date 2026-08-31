// Package toolsets holds the published names a user may select with
// PIHOLE_TOOLSETS, and the mapping from a tool name to the toolset that owns
// it.
//
// This is deliberately a leaf package: it imports nothing else from this
// module, so internal/config can validate a user's selection without any
// prospect of an import cycle through internal/tools.
//
// Membership is derived from the tool name's family token rather than from a
// hand-written list of every tool. A new pihole_stats_* endpoint therefore
// reaches every configuration that pinned "stats" with no edit here, which is
// the property that makes a toolset name safe to publish as a permanent
// contract.
package toolsets

import (
	"fmt"
	"sort"
	"strings"
)

// All is the special name meaning every toolset, an explicit synonym for
// leaving PIHOLE_TOOLSETS unset.
const All = "all"

// Toolset is one published scoping name.
//
// Name is the compatibility contract: it is what a user writes in
// PIHOLE_TOOLSETS and it can never be renamed, removed or narrowed. Display
// headings live separately, in headings, and are keyed by token rather than by
// toolset. That separation is deliberate: "search" belongs to the domains
// toolset but keeps its own "Search" heading, so deriving headings from toolset
// names would churn every anchor in the generated reference.
type Toolset struct {
	Name    string
	Tokens  []string
	Summary string
}

// table is the published set, in the order tools are registered.
var table = []Toolset{
	{"dashboard", []string{"padd"}, "One-call Pi-hole dashboard"},
	{"dns", []string{"dns", "local"}, "Blocking toggle and local DNS and CNAME records"},
	{"stats", []string{"stats"}, "Query statistics, live and long term"},
	{"info", []string{"info"}, "System, version, database and FTL diagnostics"},
	{"queries", []string{"queries"}, "Query log search and filter discovery"},
	{"history", []string{"history"}, "Activity over time, per client and overall"},
	{"domains", []string{"domains", "search"}, "Allow and deny lists, and cross-list search"},
	{"groups", []string{"groups"}, "Client and list grouping"},
	{"clients", []string{"clients"}, "Known clients and their group membership"},
	{"lists", []string{"lists"}, "Blocklist and allowlist subscriptions"},
	{"config", []string{"config"}, "Read and write Pi-hole configuration"},
	{"actions", []string{"action"}, "Gravity update, DNS restart and log flushing"},
	{"network", []string{"network"}, "Discovered devices, routes and interfaces"},
	{"dhcp", []string{"dhcp"}, "DHCP leases"},
	{"logs", []string{"logs"}, "dnsmasq, FTL and webserver logs"},
	{"teleporter", []string{"teleporter"}, "Configuration backup export and import"},
	{"sessions", []string{"auth"}, "Pi-hole API sessions"},
	{"instance", []string{"instance"}, "Compare and reconcile multiple Pi-holes"},
}

// headings maps a family token to its docs/TOOLS.md display heading. Kept
// separate from the toolset name so that "search" and "local" keep their own
// headings while belonging to a wider toolset.
var headings = map[string]string{
	"padd":       "Dashboard",
	"dns":        "DNS Blocking",
	"local":      "Local DNS",
	"stats":      "Statistics",
	"info":       "System Info",
	"queries":    "Query Log",
	"history":    "History",
	"search":     "Search",
	"domains":    "Domains",
	"groups":     "Groups",
	"clients":    "Clients",
	"lists":      "Lists",
	"config":     "Configuration",
	"action":     "Actions",
	"network":    "Network",
	"dhcp":       "DHCP",
	"logs":       "Logs",
	"teleporter": "Teleporter",
	"auth":       "Sessions",
	"instance":   "Multi-Instance",
}

// byToken indexes the table for lookup, built once at init so Of stays cheap
// enough to call per tool on every tools/list.
var byToken = func() map[string]string {
	m := make(map[string]string, len(headings))
	for _, ts := range table {
		for _, tok := range ts.Tokens {
			m[tok] = ts.Name
		}
	}
	return m
}()

// List returns the published toolsets in registration order.
func List() []Toolset {
	out := make([]Toolset, len(table))
	copy(out, table)
	return out
}

// Names returns the published toolset names, sorted, for error messages and
// documentation.
func Names() []string {
	out := make([]string, 0, len(table))
	for _, ts := range table {
		out = append(out, ts.Name)
	}
	sort.Strings(out)
	return out
}

// Token returns the family token of a tool name, which is its second
// underscore-separated component: pihole_stats_summary yields "stats". It
// returns the empty string for a name that does not have one.
func Token(toolName string) string {
	parts := strings.SplitN(toolName, "_", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// Of returns the toolset owning a tool, and whether one was found.
func Of(toolName string) (string, bool) {
	name, ok := byToken[Token(toolName)]
	return name, ok
}

// Heading returns the docs/TOOLS.md display heading for a family token.
func Heading(token string) (string, bool) {
	h, ok := headings[token]
	return h, ok
}

// Validate reports whether every name is a published toolset. The error names
// the offending value and lists every valid name, because the usual way a user
// sees it is a stdio server that exited before the client could show anything
// else.
func Validate(names []string) error {
	known := make(map[string]bool, len(table))
	for _, ts := range table {
		known[ts.Name] = true
	}
	for _, n := range names {
		if n == All || known[n] {
			continue
		}
		return fmt.Errorf("contains unknown toolset %q; valid names are: %s",
			n, strings.Join(Names(), ", "))
	}
	return nil
}

// Resolve turns a selection into the set of toolset names to expose. It
// returns nil when the selection places no restriction, which is the case for
// an empty selection and for one containing "all".
func Resolve(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	selected := make(map[string]bool, len(names))
	for _, n := range names {
		if n == All {
			return nil
		}
		selected[n] = true
	}
	return selected
}
