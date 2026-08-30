// Command toolsdoc generates docs/TOOLS.md from the registered tool
// definitions. Because it registers the real tools, the generated
// reference cannot drift from what the server serves. CI regenerates the
// file and fails on any diff.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	piholeserver "github.com/hexamatic/pihole-mcp/internal/server"
	"github.com/hexamatic/pihole-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
)

// categoryNames maps the second token of a tool name to a display heading.
var categoryNames = map[string]string{
	"padd":       "Dashboard",
	"dns":        "DNS Blocking",
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

func main() {
	out := flag.String("out", "docs/TOOLS.md", "Output path for the generated reference")
	checkE2E := flag.Bool("check-e2e", false,
		"Report registered tools the end-to-end script never exercises, instead of writing the reference")
	e2eScript := flag.String("e2e-script", "scripts/e2e-test.sh",
		"Path to the end-to-end script scanned by -check-e2e")
	flag.Parse()

	single, multiOnly := registerAll()

	if *checkE2E {
		script, err := os.ReadFile(*e2eScript)
		if err != nil {
			log.Fatalf("read %s: %v", *e2eScript, err)
		}
		registered := make([]string, 0, len(single)+len(multiOnly))
		for _, t := range single {
			registered = append(registered, t.Name)
		}
		for _, t := range multiOnly {
			registered = append(registered, t.Name)
		}
		if !reportE2ECoverage(registered, parseE2EToolNames(string(script)), e2eSkips, *e2eScript, os.Stdout) {
			os.Exit(1)
		}
		return
	}

	doc := render(single, multiOnly)
	//nolint:gosec // a committed documentation file must be world-readable
	if err := os.WriteFile(*out, []byte(doc), 0o644); err != nil {
		log.Fatalf("write %s: %v", *out, err)
	}
	fmt.Printf("wrote %s (%d tools + %d multi-instance-only)\n", *out, len(single), len(multiOnly))
}

// registerAll performs the two registration passes the reference and the
// coverage check share: one against a single instance, giving the tool set
// most users see, and one against two instances, which additionally exposes
// the multi-instance-only tools.
func registerAll() (single, multiOnly []mcp.Tool) {
	// Pass 1: single instance, the tool set most users see, without the
	// injected "instance" parameter.
	single = register(1)
	singleNames := make(map[string]bool, len(single))
	for _, t := range single {
		singleNames[t.Name] = true
	}

	// Pass 2: two instances, which captures the multi-instance-only tools.
	tools.ResetCatalogue()
	for _, t := range register(2) {
		if !singleNames[t.Name] {
			multiOnly = append(multiOnly, t)
		}
	}
	return single, multiOnly
}

// register builds a server against n fake instances and returns the
// recorded tool catalogue. Registration performs no network calls.
func register(n int) []mcp.Tool {
	instances := make([]pihole.InstanceConfig, n)
	for i := range instances {
		instances[i] = pihole.InstanceConfig{
			Name:     fmt.Sprintf("instance-%d", i+1),
			URL:      fmt.Sprintf("http://pihole-%d.invalid", i+1),
			Password: "unused",
		}
	}
	registry := pihole.NewRegistry(instances)
	defer registry.Close()
	_ = piholeserver.New(registry)
	return tools.Catalogue()
}

// e2eToolCall matches a tool name passed as the quoted first argument to any
// call_* helper in the end-to-end script: call_tool, call_multi,
// call_tool_expect and any future sibling. Matching the shape of the call
// rather than interpreting bash keeps the check working as the script's
// helpers grow.
var e2eToolCall = regexp.MustCompile(`\bcall_[A-Za-z0-9_]*[ \t]+(?:"([A-Za-z0-9_]+)"|'([A-Za-z0-9_]+)')`)

// e2eSkips records the registered tools that scripts/e2e-test.sh deliberately
// does not exercise, each against the reason it stays out. A tool is only
// allowed to be missing from the script if it is named here, so no coverage
// gap can be hidden without someone writing down why it is acceptable.
var e2eSkips = map[string]string{
	// The four batch-delete tools cannot be exercised until their payload
	// shape is corrected. Every one of them POSTs a bare JSON array of
	// identifiers, and FTL requires an array of objects. Verified against
	// Pi-hole v6 2026.07.2 on 30 August 2026: all four answered
	// "Invalid request: Batch delete requires an array of objects".
	// Add the cases in the session that fixes the shape, and delete these
	// four entries in the same change.
	"pihole_domains_batch_delete": "The tool sends a bare array of identifiers where FTL requires an array of objects, " +
		"so every call fails; an end-to-end case can only be written once the payload shape is corrected.",
	"pihole_groups_batch_delete": "Same bare-array payload defect as pihole_domains_batch_delete; the call cannot succeed " +
		"against any Pi-hole, so there is no passing case to write yet.",
	"pihole_lists_batch_delete": "Same bare-array payload defect as pihole_domains_batch_delete; the call cannot succeed " +
		"against any Pi-hole, so there is no passing case to write yet.",
	"pihole_clients_batch_delete": "Same bare-array payload defect as pihole_domains_batch_delete; the call cannot succeed " +
		"against any Pi-hole, so there is no passing case to write yet.",
	"pihole_auth_revoke_session": "Revoking a session invalidates the credentials the harness itself is authenticated with, " +
		"so every later case in the run would fail on authentication rather than on the behaviour it means to test.",
	"pihole_action_flush_logs": "Flushing the query log erases the recorded queries that the statistics, query log and " +
		"history cases read back later in the same run, so exercising it would make those cases assert against an empty database.",
	"pihole_action_flush_network": "Flushing the network table erases the device and lease records that the network and DHCP " +
		"cases read back later in the same run, so exercising it would make those cases assert against an empty table.",
	"pihole_action_gravity_update": "A gravity update re-downloads and rebuilds every configured blocklist. It takes minutes " +
		"and depends on outbound internet access that the harness cannot assume, which would make the run both slow and flaky.",
	"pihole_network_delete_device": "Deleting a device record permanently removes state that later cases and subsequent runs " +
		"against the same Pi-hole read, and the device cannot be recreated through the API to undo it. " +
		"The handler is covered by unit tests in internal/tools/network_test.go.",
}

// parseE2EToolNames returns the set of tool names the end-to-end script
// invokes. Lines that are entirely a comment are ignored, so a commented-out
// call does not count as coverage.
func parseE2EToolNames(script string) map[string]bool {
	names := map[string]bool{}
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, m := range e2eToolCall.FindAllStringSubmatch(line, -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			names[name] = true
		}
	}
	return names
}

// reportE2ECoverage writes a coverage report for the registered tools and
// reports whether the check passed. It fails when a registered tool is
// exercised by neither the script nor an entry in skips, and when a skip has
// gone stale, so the skip list cannot outlive the reason that justified it.
func reportE2ECoverage(registered []string, covered map[string]bool, skips map[string]string, scriptPath string, w io.Writer) bool {
	isRegistered := make(map[string]bool, len(registered))
	for _, name := range registered {
		isRegistered[name] = true
	}

	var uncovered, skipped []string
	for _, name := range registered {
		switch {
		case covered[name]:
		case skips[name] != "":
			skipped = append(skipped, name)
		default:
			uncovered = append(uncovered, name)
		}
	}

	var stale []string
	for name := range skips {
		switch {
		case !isRegistered[name]:
			stale = append(stale, name+" is not a registered tool")
		case covered[name]:
			stale = append(stale, name+" is exercised by the script and no longer needs a skip")
		}
	}

	sort.Strings(uncovered)
	sort.Strings(skipped)
	sort.Strings(stale)

	var b strings.Builder
	fmt.Fprintf(&b, "%d registered tools; %d exercised by %s, %d deliberately skipped, %d uncovered.\n",
		len(registered), len(registered)-len(skipped)-len(uncovered), scriptPath, len(skipped), len(uncovered))

	if len(skipped) > 0 {
		b.WriteString("\nDeliberately skipped:\n")
		for _, name := range skipped {
			fmt.Fprintf(&b, "  %s\n      %s\n", name, skips[name])
		}
	}

	if len(uncovered) > 0 {
		b.WriteString("\nNo end-to-end coverage and no recorded reason:\n")
		for _, name := range uncovered {
			fmt.Fprintf(&b, "  %s\n", name)
		}
		fmt.Fprintf(&b, "\nAdd a case to %s for each, or, if the tool genuinely cannot run against a live\n"+
			"Pi-hole, add it to e2eSkips in cmd/toolsdoc/main.go with the reason.\n", scriptPath)
	}

	if len(stale) > 0 {
		b.WriteString("\nStale entries in e2eSkips, which must be removed:\n")
		for _, entry := range stale {
			fmt.Fprintf(&b, "  %s\n", entry)
		}
	}

	ok := len(uncovered) == 0 && len(stale) == 0
	if ok {
		b.WriteString("\nEvery registered tool is either exercised or skipped for a stated reason.\n")
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		log.Fatalf("write coverage report: %v", err)
	}
	return ok
}

func render(single, multiOnly []mcp.Tool) string {
	var b strings.Builder

	b.WriteString("# Tool Reference\n\n")
	b.WriteString("<!-- Generated by `just docs-gen` (cmd/toolsdoc). Do not edit by hand. -->\n\n")
	fmt.Fprintf(&b, "%d tools with a single Pi-hole configured; %d with multiple instances.\n\n",
		len(single), len(single)+len(multiOnly))
	b.WriteString("With more than one Pi-hole configured, every tool also accepts an optional " +
		"`instance` parameter (an instance name, or `all` to aggregate across instances), " +
		"and the tools under [Multi-Instance](#multi-instance) become available.\n\n")

	// Group in registration order.
	var order []string
	grouped := map[string][]mcp.Tool{}
	add := func(ts []mcp.Tool) {
		for _, t := range ts {
			cat := category(t.Name)
			if _, seen := grouped[cat]; !seen {
				order = append(order, cat)
			}
			grouped[cat] = append(grouped[cat], t)
		}
	}
	add(single)
	add(multiOnly)

	for _, cat := range order {
		fmt.Fprintf(&b, "## %s\n\n", cat)
		for _, t := range grouped[cat] {
			writeTool(&b, t)
		}
	}
	return b.String()
}

func category(name string) string {
	parts := strings.SplitN(name, "_", 3)
	if len(parts) < 2 {
		return "Other"
	}
	if display, ok := categoryNames[parts[1]]; ok {
		return display
	}
	return "Other"
}

func writeTool(b *strings.Builder, t mcp.Tool) {
	fmt.Fprintf(b, "### `%s`\n\n", t.Name)

	var badges []string
	if t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint {
		badges = append(badges, "read-only")
	}
	if t.Annotations.DestructiveHint != nil && *t.Annotations.DestructiveHint {
		badges = append(badges, "destructive")
	}
	if t.OutputSchema.Type != "" || t.RawOutputSchema != nil {
		badges = append(badges, "structured output")
	}
	if len(badges) > 0 {
		fmt.Fprintf(b, "*%s*\n\n", strings.Join(badges, " · "))
	}

	if t.Description != "" {
		fmt.Fprintf(b, "%s\n\n", t.Description)
	}

	writeParams(b, t)
}

func writeParams(b *strings.Builder, t mcp.Tool) {
	props := t.InputSchema.Properties
	if len(props) == 0 {
		b.WriteString("_No parameters._\n\n")
		return
	}

	required := make(map[string]bool, len(t.InputSchema.Required))
	for _, r := range t.InputSchema.Required {
		required[r] = true
	}

	// Required parameters first (schema order), then optional alphabetically.
	names := append([]string(nil), t.InputSchema.Required...)
	var optional []string
	for name := range props {
		// The injected multi-instance routing parameter is documented once
		// in the header, not per tool.
		if name == "instance" || required[name] {
			continue
		}
		optional = append(optional, name)
	}
	sort.Strings(optional)
	names = append(names, optional...)

	b.WriteString("| Parameter | Type | Required | Description |\n|---|---|---|---|\n")
	for _, name := range names {
		prop, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		req := "No"
		if required[name] {
			req = "Yes"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n",
			name, cell(propType(prop)), req, cell(propDescription(prop)))
	}
	b.WriteString("\n")
}

// propType renders the parameter type, folding in enum options.
func propType(prop map[string]any) string {
	typ, _ := prop["type"].(string)
	if typ == "" {
		typ = "any"
	}
	if enum, ok := prop["enum"].([]any); ok && len(enum) > 0 {
		opts := make([]string, len(enum))
		for i, v := range enum {
			opts[i] = fmt.Sprintf("`%v`", v)
		}
		return typ + " (" + strings.Join(opts, ", ") + ")"
	}
	return typ
}

// propDescription renders the description, folding in the default value.
func propDescription(prop map[string]any) string {
	desc, _ := prop["description"].(string)
	if def, ok := prop["default"]; ok {
		if desc != "" && !strings.HasSuffix(desc, ".") {
			desc += "."
		}
		desc = strings.TrimSpace(desc + fmt.Sprintf(" Default: `%v`.", def))
	}
	return desc
}

// cell escapes a value for use inside a Markdown table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}
