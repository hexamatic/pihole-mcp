package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterLists registers blocklist/allowlist management tools.
func RegisterLists(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_lists_list",
		mcp.WithTitleAnnotation("List Blocklists"),
		mcp.WithDescription("List configured blocklists and allowlists with domain counts and update status. Filter by type (allow/block)."),
		mcp.WithString("type", mcp.Description("Filter: 'allow' or 'block'."), mcp.Enum("allow", "block")),
		detailParam,
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), listsListHandler(r))

	addTool(s, r, mcp.NewTool("pihole_lists_add",
		mcp.WithTitleAnnotation("Add List"),
		mcp.WithDescription("Subscribe to a new blocklist or allowlist URL. Run pihole_action_gravity_update afterwards to download it."),
		mcp.WithString("address", mcp.Required(), mcp.Description("URL of the list.")),
		mcp.WithString("type", mcp.Required(), mcp.Description("'allow' or 'block'."), mcp.Enum("allow", "block")),
		mcp.WithString("comment", mcp.Description("Comment for the list.")),
		mcp.WithBoolean("enabled", mcp.Description("Enabled state (default true).")),
		mcp.WithOpenWorldHintAnnotation(true),
	), listsAddHandler(r))

	addTool(s, r, mcp.NewTool("pihole_lists_update",
		mcp.WithTitleAnnotation("Update List"),
		mcp.WithDescription("Update a blocklist or allowlist entry's comment, enabled status, or group assignments."),
		mcp.WithString("address", mcp.Required(), mcp.Description("URL of the list.")),
		mcp.WithString("type", mcp.Required(), mcp.Description("'allow' or 'block'."), mcp.Enum("allow", "block")),
		mcp.WithString("comment", mcp.Description("Updated comment.")),
		mcp.WithBoolean("enabled", mcp.Description("Updated enabled status.")),
		mcp.WithIdempotentHintAnnotation(true),
	), listsUpdateHandler(r))

	addTool(s, r, mcp.NewTool("pihole_lists_delete",
		mcp.WithTitleAnnotation("Delete List"),
		mcp.WithDescription("Unsubscribe from a blocklist or allowlist. Run pihole_action_gravity_update afterwards to apply changes."),
		mcp.WithString("address", mcp.Required(), mcp.Description("URL to remove.")),
		mcp.WithString("type", mcp.Required(), mcp.Description("'allow' or 'block'."), mcp.Enum("allow", "block")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), listsDeleteHandler(r))

	addTool(s, r, mcp.NewTool("pihole_lists_batch_delete",
		mcp.WithTitleAnnotation("Batch Delete Lists"),
		mcp.WithDescription("Unsubscribe from multiple lists at once. Each item needs URL and type (allow/block)."),
		mcp.WithString("items", mcp.Required(), mcp.Description("JSON array: [{\"item\":\"url\",\"type\":\"block\"}]")),
		mcp.WithDestructiveHintAnnotation(true),
	), listsBatchDeleteHandler(r))
}

func listsListHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path := "/lists"
		if t := req.GetString("type", ""); t != "" {
			path += "?type=" + t
		}

		var result pihole.ListsResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("list lists", err), nil
		}

		if len(result.Lists) == 0 {
			return mcp.NewToolResultText("No lists found."), nil
		}

		detail := getDetail(req)

		if detail == "minimal" {
			return mcp.NewToolResultText(fmt.Sprintf("%d lists.", len(result.Lists))), nil
		}

		if wantCSV(req) {
			headers := []string{"Address", "Type", "Domains", "Enabled", "Comment"}
			rows := make([][]string, 0, len(result.Lists))
			for _, l := range result.Lists {
				rows = append(rows, []string{l.Address, l.Type, format.Number(l.Number), format.Bool(l.Enabled), l.Comment})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d lists:**\n", len(result.Lists))
		for _, l := range result.Lists {
			status := "enabled"
			if !l.Enabled {
				status = "disabled"
			}
			fmt.Fprintf(&b, "- %s (%s, %s domains, %s)", l.Address, l.Type, format.Number(l.Number), status)
			if l.Comment != "" {
				fmt.Fprintf(&b, " — %s", l.Comment)
			}
			if detail == "full" {
				fmt.Fprintf(&b, " [id=%d, updated=%s, invalid=%d]", l.ID, format.Timestamp(float64(l.DateUpdated)), l.InvalidDomains)
			}
			b.WriteString("\n")
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func listsAddHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		address, _ := req.RequireString("address")
		t, _ := req.RequireString("type")

		if err := validateURL(address); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid address: %v", err)), nil
		}

		body := map[string]any{"address": address}
		if comment := req.GetString("comment", ""); comment != "" {
			if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			body["comment"] = comment
		}
		if !req.GetBool("enabled", true) {
			body["enabled"] = false
		}

		path := "/lists?type=" + t
		var result pihole.ListsResponse
		if err := c.Post(ctx, path, body, &result); err != nil {
			return toolError("add list", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Added** %s list: %s. Run pihole_action_gravity_update to download.", t, address)), nil
	}
}

func listsUpdateHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		address, _ := req.RequireString("address")
		t, _ := req.RequireString("type")

		if err := validateURL(address); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid address: %v", err)), nil
		}

		body := make(map[string]any)
		if comment := req.GetString("comment", ""); comment != "" {
			if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			body["comment"] = comment
		}
		if enabled := req.GetBool("enabled", true); !enabled {
			body["enabled"] = false
		}
		body["type"] = t

		path := "/lists/" + address + "?type=" + t
		var result pihole.ListsResponse
		if err := c.Put(ctx, path, body, &result); err != nil {
			return toolError("update list", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Updated** list: %s.", address)), nil
	}
}

func listsDeleteHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		address, _ := req.RequireString("address")
		t, _ := req.RequireString("type")

		if err := validateURL(address); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid address: %v", err)), nil
		}

		path := "/lists/" + address + "?type=" + t
		if err := c.Delete(ctx, path); err != nil {
			return toolError("delete list", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Deleted** list: %s.", address)), nil
	}
}

func listsBatchDeleteHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		items, err := req.RequireString("items")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'items' is required (JSON array)"), nil
		}

		if err := c.Post(ctx, "/lists:batchDelete", rawJSON(items), nil); err != nil {
			return toolError("batch delete lists", err), nil
		}

		return mcp.NewToolResultText("**Batch delete completed.**"), nil
	}
}
