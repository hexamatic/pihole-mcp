package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterConfig registers Pi-hole configuration tools.
func RegisterConfig(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_config_get",
		mcp.WithTitleAnnotation("Get Configuration"),
		mcp.WithDescription("Get Pi-hole configuration as dotted key/value lines. Name a section (dns, webserver, dhcp) for a subset, or omit for everything. detail=minimal lists section names; detail=full returns raw JSON."),
		mcp.WithString("section", mcp.Description("Config section: dns, webserver, dhcp, files, misc, etc.")),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), configGetHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_set",
		mcp.WithTitleAnnotation("Set Configuration"),
		mcp.WithDescription("Modify Pi-hole configuration. Provide nested JSON properties to change. Changes take effect immediately and can affect DNS behaviour system-wide."),
		mcp.WithString("config", mcp.Required(), mcp.Description("JSON config object, e.g. {\"dns\":{\"blocking\":{\"active\":true}}}")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true). Set false when chaining several config_set calls, so only the last one pays for the restart.")),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	), configSetHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_get_value",
		mcp.WithTitleAnnotation("Get Config Value"),
		mcp.WithDescription("Get a specific configuration value by dotted path (e.g. dns.upstreams, webserver.port, dhcp.active)."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams, dns.hosts (local DNS records) or dns.cnameRecords.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), configGetValueHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_add_value",
		mcp.WithTitleAnnotation("Add Config Array Value"),
		mcp.WithDescription("Add a value to a configuration array such as dns.upstreams. For dns.hosts and dns.cnameRecords prefer pihole_local_dns_add and pihole_local_cname_add, which take structured arguments."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams, dns.hosts or dns.cnameRecords.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to add.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithIdempotentHintAnnotation(true),
	), configAddValueHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_remove_value",
		mcp.WithTitleAnnotation("Remove Config Array Value"),
		mcp.WithDescription("Remove a value from a configuration array such as dns.upstreams. For dns.hosts and dns.cnameRecords prefer pihole_local_dns_delete and pihole_local_cname_delete, which take structured arguments."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams, dns.hosts or dns.cnameRecords.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to remove.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), configRemoveValueHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_properties",
		mcp.WithTitleAnnotation("List Read-Only Config Keys"),
		mcp.WithDescription("List Pi-hole config keys that are read-only — set via pihole.toml or env var, not modifiable through the API. Use after a pihole_config_set error to confirm whether a key is intentionally locked."),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), configPropertiesHandler(r))
}

func configGetHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path := "/config"
		if section := req.GetString("section", ""); section != "" {
			if err := validateMaxLength("section", section, maxConfigPathLen); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			// A section is normally one component, but escapeConfigElement also
			// handles a dotted path without turning its separators into %2F.
			path += "/" + escapeConfigElement(section)
		}

		var result pihole.ConfigResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get config", err), nil
		}

		detail := getDetail(req)

		if detail == "minimal" {
			sections := make([]string, 0, len(result.Config))
			for k := range result.Config {
				sections = append(sections, k)
			}
			sort.Strings(sections)
			return mcp.NewToolResultText(fmt.Sprintf("Config sections: %s", strings.Join(sections, ", "))), nil
		}

		if detail == "normal" {
			// One dotted line per setting. The old normal detail counted the
			// keys in each section and printed the counts, so a tool whose
			// description promises the full config could not emit a single
			// configuration value at the level callers get by default.
			flat := flattenTree(result.Config)
			if flat == "" {
				return mcp.NewToolResultText("No configuration returned."), nil
			}
			return mcp.NewToolResultText(flat), nil
		}

		// full: JSON dump
		configJSON, err := json.MarshalIndent(result.Config, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to format config: %v", err)), nil
		}

		var b strings.Builder
		b.WriteString("```json\n")
		b.Write(configJSON)
		b.WriteString("\n```")

		return mcp.NewToolResultText(b.String()), nil
	}
}

func configSetHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		configStr, err := req.RequireString("config")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'config' is required (JSON object)"), nil
		}

		// The Pi-hole API requires the update body wrapped in a "config" key
		// ({"config": {...}}). The tool parameter is the bare config object, so
		// wrap it before sending. Validate the JSON first so bad input surfaces
		// as a clear tool error rather than a 400 from the API.
		var configObj any
		if err := json.Unmarshal([]byte(configStr), &configObj); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Parameter 'config' must be valid JSON: %v", err)), nil
		}

		payload, ok := configObj.(map[string]any)
		if !ok {
			return mcp.NewToolResultError(`Parameter 'config' must be a JSON object, e.g. {"dns":{"blocking":{"active":true}}}`), nil
		}

		// Accept a payload that already carries the API envelope. Pi-hole has no
		// top-level "config" section, so a lone "config" key can only be the
		// wrapper itself. Wrapping it a second time is the dangerous case: FTL
		// ignores keys it does not recognise and still answers 200, so the write
		// would silently apply nothing.
		// Unwrap as many envelopes as the caller supplied. Someone working
		// around the original bug by adding the key themselves can easily add
		// it to a payload that already had it, and one unwrap would then leave
		// a doubled envelope on the wire for FTL to ignore.
		for len(payload) == 1 {
			inner, wrapped := payload["config"].(map[string]any)
			if !wrapped {
				break
			}
			payload = inner
		}

		path := "/config"
		if !req.GetBool("restart", true) {
			path += "?restart=false"
		}

		var result pihole.ConfigResponse
		if err := c.Do(ctx, "PATCH", path, map[string]any{"config": payload}, &result); err != nil {
			return toolError("update config", err), nil
		}

		sendLog(ctx, mcp.LoggingLevelInfo, "config", map[string]any{"instance": c.Name(), "event": "config_updated"})

		configJSON, _ := json.MarshalIndent(result.Config, "", "  ")

		var b strings.Builder
		b.WriteString("**Config updated.**\n```json\n")
		b.Write(configJSON)
		b.WriteString("\n```")

		return mcp.NewToolResultText(b.String()), nil
	}
}

func configGetValueHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		element, err := req.RequireString("element")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'element' is required"), nil
		}
		if err := validateMaxLength("element", element, maxConfigPathLen); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		path := "/config/" + escapeConfigElement(element)

		var result pihole.ConfigResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get config value", err), nil
		}

		// FTL nests the requested item under its full path from the root, so
		// dns.hosts comes back as {"config":{"dns":{"hosts":[...]}}}. The
		// element is a filter over what is included, never a change of root,
		// so walk down to the leaf before rendering it. Printing result.Config
		// straight out returns the wrapper the user did not ask for.
		value := unwrapConfigValue(result.Config, element)

		// Format the value — use JSON for complex types, plain text for scalars.
		var formatted string
		if len(value) == 1 {
			for _, v := range value {
				switch v.(type) {
				case string, float64, bool, nil:
					formatted = fmt.Sprintf("%v", v)
				default:
					j, _ := json.Marshal(v)
					formatted = string(j)
				}
			}
		} else {
			j, _ := json.Marshal(value)
			formatted = string(j)
		}

		return mcp.NewToolResultText(fmt.Sprintf("**%s:** %s", element, formatted)), nil
	}
}

func configAddValueHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		element, err := req.RequireString("element")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'element' is required"), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'value' is required"), nil
		}
		if err := validateMaxLength("element", element, maxConfigPathLen); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		if err := validateMaxLength("value", value, maxCommentLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		path := "/config/" + escapeConfigElement(element) + "/" + pihole.EscapePathSegment(value)

		if !req.GetBool("restart", true) {
			path += "?restart=false"
		}

		var result pihole.ConfigResponse
		if err := c.Put(ctx, path, nil, &result); err != nil {
			return toolError("add config value", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Added** `%s` to `%s`.", value, element)), nil
	}
}

func configRemoveValueHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		element, err := req.RequireString("element")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'element' is required"), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'value' is required"), nil
		}
		if err := validateMaxLength("element", element, maxConfigPathLen); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		if err := validateMaxLength("value", value, maxCommentLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		path := "/config/" + escapeConfigElement(element) + "/" + pihole.EscapePathSegment(value)

		if !req.GetBool("restart", true) {
			path += "?restart=false"
		}

		if err := c.Do(ctx, "DELETE", path, nil, nil); err != nil {
			return toolError("remove config value", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Removed** `%s` from `%s`.", value, element)), nil
	}
}

func configPropertiesHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.ConfigPropertiesResponse
		if err := c.Get(ctx, "/config/_properties", &result); err != nil {
			return toolError("list config properties (requires Pi-hole FTL v6.6.1+)", err), nil
		}

		ros := result.Config.ReadOnly
		sort.Slice(ros, func(i, j int) bool { return ros[i].Key < ros[j].Key })
		if len(ros) == 0 {
			return mcp.NewToolResultText("No read-only config keys reported."), nil
		}

		if wantCSV(req) {
			headers := []string{"Key", "Reason", "Description"}
			rows := make([][]string, 0, len(ros))
			for _, p := range ros {
				rows = append(rows, []string{p.Key, p.Reason, p.Description})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d read-only config keys:**\n", len(ros))
		for _, p := range ros {
			fmt.Fprintf(&b, "- `%s` (%s) — %s\n", p.Key, p.Reason, p.Description)
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

// unwrapConfigValue walks a config response down the dotted element path to
// the value the caller asked for. FTL builds the body from the root of the
// config tree, so a request for dns.hosts arrives wrapped in one object per
// path component. Anything that does not match that shape is returned as it
// came, so an unexpected body is still rendered rather than swallowed.
func unwrapConfigValue(cfg map[string]any, element string) map[string]any {
	out := cfg
	parts := strings.Split(element, ".")
	for i, p := range parts {
		v, ok := out[p]
		if !ok {
			return cfg
		}
		if i == len(parts)-1 {
			return map[string]any{p: v}
		}
		next, ok := v.(map[string]any)
		if !ok {
			return cfg
		}
		out = next
	}
	return out
}
