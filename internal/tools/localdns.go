package tools

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Pi-hole stores local DNS and CNAME records as opaque strings in two config
// arrays, dns.hosts and dns.cnameRecords, and addresses an individual entry by
// putting that whole string in the URL path. A host record is "<ip> <hostname>"
// and so always contains a space, which is exactly the shape that the old path
// concatenation mishandled; pihole.EscapePathSegment is what makes it safe.
const (
	hostsField  = "hosts"
	cnamesField = "cnameRecords"

	// maxTTL bounds the optional CNAME TTL. dnsmasq accepts a 32-bit value;
	// anything beyond a day is already indistinguishable from permanent for a
	// local record, but the bound exists to reject nonsense rather than to
	// express policy.
	maxTTL = 604800
)

// RegisterLocalDNS registers the local DNS and CNAME record tools.
//
// These are split per verb rather than bundled as one tool with an action
// argument, because ReadOnlyHint is a per-tool annotation: a single tool that
// both lists and deletes is a write tool, and PIHOLE_READ_ONLY would then hide
// the ability to read local DNS records at all. The split also matches the
// domains, groups, clients and lists families.
func RegisterLocalDNS(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_local_dns_list",
		mcp.WithTitleAnnotation("List Local DNS Records"),
		mcp.WithDescription("List local A and AAAA records this Pi-hole answers for, from dns.hosts. Each record maps an IP address to a hostname."),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), localRecordsListHandler(r, hostsField, "local DNS"))

	addTool(s, r, mcp.NewTool("pihole_local_dns_add",
		mcp.WithTitleAnnotation("Add Local DNS Record"),
		mcp.WithDescription("Add a local A or AAAA record, so this Pi-hole resolves a hostname to an IP address. The record type follows from the address family."),
		mcp.WithString("ip", mcp.Required(), mcp.Description("IPv4 or IPv6 address, e.g. 192.168.1.50 or fd00::1.")),
		mcp.WithString("hostname", mcp.Required(), mcp.Description("Hostname to resolve, e.g. nas.home.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true). Set false when adding several records, so only the last one pays for the restart.")),
		mcp.WithIdempotentHintAnnotation(true),
	), localDNSAddHandler(r))

	addTool(s, r, mcp.NewTool("pihole_local_dns_delete",
		mcp.WithTitleAnnotation("Delete Local DNS Record"),
		mcp.WithDescription("Delete a local A or AAAA record by IP address and hostname. Pi-hole stops resolving that hostname locally."),
		mcp.WithString("ip", mcp.Required(), mcp.Description("IP address of the record to remove.")),
		mcp.WithString("hostname", mcp.Required(), mcp.Description("Hostname of the record to remove.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), localDNSDeleteHandler(r))

	addTool(s, r, mcp.NewTool("pihole_local_cname_list",
		mcp.WithTitleAnnotation("List Local CNAME Records"),
		mcp.WithDescription("List local CNAME records from dns.cnameRecords. Each record points an alias at another name this Pi-hole can resolve."),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), localRecordsListHandler(r, cnamesField, "local CNAME"))

	addTool(s, r, mcp.NewTool("pihole_local_cname_add",
		mcp.WithTitleAnnotation("Add Local CNAME Record"),
		mcp.WithDescription("Add a local CNAME record pointing an alias at another name. The target must be a name Pi-hole can already resolve."),
		mcp.WithString("alias", mcp.Required(), mcp.Description("Alias to create, e.g. files.home.")),
		mcp.WithString("target", mcp.Required(), mcp.Description("Name the alias resolves to, e.g. nas.home.")),
		mcp.WithNumber("ttl", mcp.Description("Optional time to live in seconds. Omit to use the Pi-hole default."),
			mcp.Min(0), mcp.Max(maxTTL)),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithIdempotentHintAnnotation(true),
	), localCNAMEAddHandler(r))

	addTool(s, r, mcp.NewTool("pihole_local_cname_delete",
		mcp.WithTitleAnnotation("Delete Local CNAME Record"),
		mcp.WithDescription("Delete a local CNAME record by its alias. Pi-hole stops resolving that alias."),
		mcp.WithString("alias", mcp.Required(), mcp.Description("Alias of the record to remove.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), localCNAMEDeleteHandler(r))
}

// readLocalRecords fetches one of the dns config arrays.
//
// FTL nests a config item under its full path from the root, so a request for
// the dns section comes back as {"config":{"dns":{"hosts":[...]}}}. Reading the
// field straight off the wrapper is the bug that made pihole_instance_sync
// report every real Pi-hole as having no local records at all.
func readLocalRecords(ctx context.Context, c *pihole.Client, field string) ([]string, error) {
	var resp pihole.ConfigResponse
	if err := c.Get(ctx, "/config/dns", &resp); err != nil {
		return nil, err
	}
	dns, _ := resp.Config["dns"].(map[string]any)
	raw, _ := dns[field].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// recordPath builds the API path addressing one entry of a dns config array.
func recordPath(field, record string, restart bool) string {
	path := "/config/dns/" + field + "/" + pihole.EscapePathSegment(record)
	if !restart {
		path += "?restart=false"
	}
	return path
}

func localRecordsListHandler(r *pihole.Registry, field, label string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		records, err := readLocalRecords(ctx, c, field)
		if err != nil {
			return toolError("list "+label+" records", err), nil
		}
		if len(records) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No %s records configured.", label)), nil
		}

		var b strings.Builder
		if wantCSV(req) {
			b.WriteString("record\n")
			for _, rec := range records {
				b.WriteString(rec + "\n")
			}
			return mcp.NewToolResultText(b.String()), nil
		}

		fmt.Fprintf(&b, "**%d %s record(s):**\n", len(records), label)
		for _, rec := range records {
			b.WriteString("- `" + rec + "`\n")
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

func localDNSAddHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ip, err := req.RequireString("ip")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'ip' is required"), nil
		}
		hostname, err := req.RequireString("hostname")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'hostname' is required"), nil
		}
		addr, err := validateIPAddress("ip", ip)
		if err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		if err := validateExactDomain(hostname); err != nil {
			return mcp.NewToolResultError("Invalid hostname: " + err.Error()), nil
		}

		record := addr.String() + " " + hostname
		if err := c.Put(ctx, recordPath(hostsField, record, req.GetBool("restart", true)), nil, nil); err != nil {
			return toolError("add local DNS record", err), nil
		}

		kind := "A"
		if !addr.Is4() {
			kind = "AAAA"
		}
		return mcp.NewToolResultText(fmt.Sprintf("**Added** %s record `%s` → `%s`.", kind, hostname, addr)), nil
	}
}

func localDNSDeleteHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ip, err := req.RequireString("ip")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'ip' is required"), nil
		}
		hostname, err := req.RequireString("hostname")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'hostname' is required"), nil
		}
		addr, err := validateIPAddress("ip", ip)
		if err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		records, err := readLocalRecords(ctx, c, hostsField)
		if err != nil {
			return toolError("read local DNS records", err), nil
		}
		match, res := resolveOne(records, describeHost(addr.String(), hostname), func(rec string) bool {
			fields := strings.Fields(rec)
			if len(fields) < 2 || fields[0] != addr.String() {
				return false
			}
			for _, name := range fields[1:] {
				if strings.EqualFold(name, hostname) {
					return true
				}
			}
			return false
		})
		if res != nil {
			return res, nil
		}

		if err := c.Delete(ctx, recordPath(hostsField, match, req.GetBool("restart", true))); err != nil {
			return toolError("delete local DNS record", err), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("**Deleted** local DNS record `%s`.", match)), nil
	}
}

func localCNAMEAddHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		alias, err := req.RequireString("alias")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'alias' is required"), nil
		}
		target, err := req.RequireString("target")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'target' is required"), nil
		}
		if err := validateExactDomain(alias); err != nil {
			return mcp.NewToolResultError("Invalid alias: " + err.Error()), nil
		}
		if err := validateExactDomain(target); err != nil {
			return mcp.NewToolResultError("Invalid target: " + err.Error()), nil
		}

		record := alias + "," + target
		if ttl := req.GetInt("ttl", -1); ttl >= 0 {
			if err := validateIntRange("ttl", ttl, 0, maxTTL); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			record += "," + strconv.Itoa(ttl)
		}

		if err := c.Put(ctx, recordPath(cnamesField, record, req.GetBool("restart", true)), nil, nil); err != nil {
			return toolError("add local CNAME record", err), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("**Added** CNAME `%s` → `%s`.", alias, target)), nil
	}
}

func localCNAMEDeleteHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		alias, err := req.RequireString("alias")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'alias' is required"), nil
		}

		records, err := readLocalRecords(ctx, c, cnamesField)
		if err != nil {
			return toolError("read local CNAME records", err), nil
		}
		match, res := resolveOne(records, "CNAME alias "+alias, func(rec string) bool {
			parts := strings.SplitN(rec, ",", 2)
			return strings.EqualFold(strings.TrimSpace(parts[0]), alias)
		})
		if res != nil {
			return res, nil
		}

		if err := c.Delete(ctx, recordPath(cnamesField, match, req.GetBool("restart", true))); err != nil {
			return toolError("delete local CNAME record", err), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("**Deleted** local CNAME record `%s`.", match)), nil
	}
}

func describeHost(ip, hostname string) string {
	return "local DNS record " + ip + " " + hostname
}

// resolveOne finds the single stored record a delete refers to, and returns an
// error result when it cannot.
//
// Deleting by a reconstructed string would be simpler and wrong. A host record
// may legitimately carry several names for one address ("10.0.0.5 nas nas.home"),
// so the value the user described is not the value FTL stored, and a DELETE of
// the reconstruction 404s while the record stays put. Reading first and deleting
// the exact stored string is also what turns "the API answered 200" into "the
// record I meant is gone".
func resolveOne(records []string, description string, match func(string) bool) (string, *mcp.CallToolResult) {
	var found []string
	for _, rec := range records {
		if match(rec) {
			found = append(found, rec)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", mcp.NewToolResultError(fmt.Sprintf(
			"No %s is configured, so nothing was deleted. List the current records to see what is there.", description))
	default:
		return "", mcp.NewToolResultError(fmt.Sprintf(
			"%s matches %d stored records (%s), so nothing was deleted. Remove the exact value with pihole_config_remove_value.",
			description, len(found), strings.Join(found, "; ")))
	}
}

// validateIPAddress parses an IP address argument, returning the parsed form so
// callers get FTL a canonical spelling rather than whatever the model typed.
func validateIPAddress(name, s string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%s: %q is not an IP address (expected something like 192.168.1.50 or fd00::1)", name, s)
	}
	return addr.Unmap(), nil
}
