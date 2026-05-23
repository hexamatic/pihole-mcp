package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterConfig registers Pi-hole configuration tools.
func RegisterConfig(s *server.MCPServer, c *pihole.Client) {
	addTool(s, mcp.NewTool("pihole_config_get",
		mcp.WithDescription("Get Pi-hole configuration. Specify a section (dns, webserver, dhcp, etc.) for a subset, or omit for full config."),
		mcp.WithString("section", mcp.Description("Config section: dns, webserver, dhcp, files, misc, etc.")),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), configGetHandler(c))

	addTool(s, mcp.NewTool("pihole_config_set",
		mcp.WithDescription("Modify Pi-hole configuration. Provide nested JSON properties to change. Changes take effect immediately."),
		mcp.WithString("config", mcp.Required(), mcp.Description("JSON config object, e.g. {\"dns\":{\"blocking\":{\"active\":true}}}")),
		mcp.WithIdempotentHintAnnotation(true),
	), configSetHandler(c))

	addTool(s, mcp.NewTool("pihole_config_get_value",
		mcp.WithDescription("Get a specific configuration value by dotted path (e.g. dns.upstreams, webserver.port, dhcp.active)."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams or dns/upstreams.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), configGetValueHandler(c))

	addTool(s, mcp.NewTool("pihole_config_add_value",
		mcp.WithDescription("Add a value to a configuration array (e.g. add an upstream DNS server). Set restart=false to defer FTL restart."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to add.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithIdempotentHintAnnotation(true),
	), configAddValueHandler(c))

	addTool(s, mcp.NewTool("pihole_config_remove_value",
		mcp.WithDescription("Remove a value from a configuration array (e.g. remove an upstream DNS server). Set restart=false to defer FTL restart."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to remove.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), configRemoveValueHandler(c))
}

func configGetHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path := "/config"
		if section := req.GetString("section", ""); section != "" {
			if err := validateMaxLength("section", section, maxConfigPathLen); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			path += "/" + section
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
			return mcp.NewToolResultText(fmt.Sprintf("Config sections: %s", strings.Join(sections, ", "))), nil
		}

		if detail == "normal" {
			var b strings.Builder
			for section, value := range result.Config {
				switch v := value.(type) {
				case map[string]any:
					fmt.Fprintf(&b, "**%s:** %d settings\n", section, len(v))
				case []any:
					fmt.Fprintf(&b, "**%s:** %d items\n", section, len(v))
				default:
					fmt.Fprintf(&b, "**%s:** %v\n", section, v)
				}
			}
			return mcp.NewToolResultText(b.String()), nil
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

func configSetHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		configStr, err := req.RequireString("config")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'config' is required (JSON object)"), nil
		}

		var result pihole.ConfigResponse
		if err := c.Do(ctx, "PATCH", "/config", rawJSON(configStr), &result); err != nil {
			return toolError("update config", err), nil
		}

		configJSON, _ := json.MarshalIndent(result.Config, "", "  ")

		var b strings.Builder
		b.WriteString("**Config updated.**\n```json\n")
		b.Write(configJSON)
		b.WriteString("\n```")

		return mcp.NewToolResultText(b.String()), nil
	}
}

func configGetValueHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		element, err := req.RequireString("element")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'element' is required"), nil
		}
		if err := validateMaxLength("element", element, maxConfigPathLen); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		element = strings.ReplaceAll(element, ".", "/")
		path := "/config/" + element

		var result pihole.ConfigResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get config value", err), nil
		}

		// Format the value — use JSON for complex types, plain text for scalars.
		var formatted string
		if len(result.Config) == 1 {
			for _, v := range result.Config {
				switch v.(type) {
				case string, float64, bool, nil:
					formatted = fmt.Sprintf("%v", v)
				default:
					j, _ := json.Marshal(v)
					formatted = string(j)
				}
			}
		} else {
			j, _ := json.Marshal(result.Config)
			formatted = string(j)
		}

		return mcp.NewToolResultText(fmt.Sprintf("**%s:** %s", element, formatted)), nil
	}
}

func configAddValueHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		element = strings.ReplaceAll(element, ".", "/")
		path := "/config/" + element + "/" + value

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

func configRemoveValueHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		element = strings.ReplaceAll(element, ".", "/")
		path := "/config/" + element + "/" + value

		if !req.GetBool("restart", true) {
			path += "?restart=false"
		}

		if err := c.Do(ctx, "DELETE", path, nil, nil); err != nil {
			return toolError("remove config value", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Removed** `%s` from `%s`.", value, element)), nil
	}
}
