package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterGroups registers group management tools.
func RegisterGroups(s *server.MCPServer, c *pihole.Client) {
	addTool(s, mcp.NewTool("pihole_groups_list",
		mcp.WithDescription("List groups used for organising domains and clients into sets with independent blocking rules."),
		mcp.WithString("name", mcp.Description("Specific group name to look up.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), groupsListHandler(c))

	addTool(s, mcp.NewTool("pihole_groups_add",
		mcp.WithDescription("Create a group for organising domains and clients into sets with independent blocking rules."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Group name.")),
		mcp.WithString("comment", mcp.Description("Optional comment.")),
		mcp.WithBoolean("enabled", mcp.Description("Enabled state (default true).")),
		mcp.WithOpenWorldHintAnnotation(true),
	), groupsAddHandler(c))

	addTool(s, mcp.NewTool("pihole_groups_update",
		mcp.WithDescription("Update or rename a group. Changing the name updates all associated domain and client references."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Current group name.")),
		mcp.WithString("new_name", mcp.Description("New name (rename).")),
		mcp.WithString("comment", mcp.Description("Updated comment.")),
		mcp.WithBoolean("enabled", mcp.Description("Updated enabled status.")),
		mcp.WithIdempotentHintAnnotation(true),
	), groupsUpdateHandler(c))

	addTool(s, mcp.NewTool("pihole_groups_delete",
		mcp.WithDescription("Delete a group by name. Domains and clients assigned to this group lose the association."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Group name to delete.")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), groupsDeleteHandler(c))

	addTool(s, mcp.NewTool("pihole_groups_batch_delete",
		mcp.WithDescription("Delete multiple groups at once. Provide a JSON array of group names."),
		mcp.WithString("items", mcp.Required(), mcp.Description("JSON array: [{\"item\":\"group_name\"}]")),
		mcp.WithDestructiveHintAnnotation(true),
	), groupsBatchDeleteHandler(c))
}

func groupsListHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path := "/groups"
		if name := req.GetString("name", ""); name != "" {
			path += "/" + name
		}

		var result pihole.GroupsResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("list groups", err), nil
		}

		if len(result.Groups) == 0 {
			return mcp.NewToolResultText("No groups found."), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d groups:**\n", len(result.Groups))
		for _, g := range result.Groups {
			status := "enabled"
			if !g.Enabled {
				status = "disabled"
			}
			fmt.Fprintf(&b, "- %s (id=%d, %s)", g.Name, g.ID, status)
			if g.Comment != "" {
				fmt.Fprintf(&b, " — %s", g.Comment)
			}
			b.WriteString("\n")
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func groupsAddHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.RequireString("name")

		if err := validateMaxLength("name", name, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		body := map[string]any{"name": name}
		if comment := req.GetString("comment", ""); comment != "" {
			if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			body["comment"] = comment
		}
		if !req.GetBool("enabled", true) {
			body["enabled"] = false
		}

		var result pihole.GroupsResponse
		if err := c.Post(ctx, "/groups", body, &result); err != nil {
			return toolError("add group", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Created** group %s.", name)), nil
	}
}

func groupsUpdateHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.RequireString("name")

		if err := validateMaxLength("name", name, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		body := make(map[string]any)
		if newName := req.GetString("new_name", ""); newName != "" {
			if err := validateMaxLength("new_name", newName, maxNameLength); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			body["name"] = newName
		}
		if comment := req.GetString("comment", ""); comment != "" {
			if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			body["comment"] = comment
		}
		if enabled := req.GetBool("enabled", true); !enabled {
			body["enabled"] = false
		}

		var result pihole.GroupsResponse
		if err := c.Put(ctx, "/groups/"+name, body, &result); err != nil {
			return toolError("update group", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Updated** group %s.", name)), nil
	}
}

func groupsDeleteHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.RequireString("name")

		if err := validateMaxLength("name", name, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		if err := c.Delete(ctx, "/groups/"+name); err != nil {
			return toolError("delete group", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Deleted** group %s.", name)), nil
	}
}

func groupsBatchDeleteHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		items, err := req.RequireString("items")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'items' is required (JSON array)"), nil
		}

		if err := c.Post(ctx, "/groups:batchDelete", rawJSON(items), nil); err != nil {
			return toolError("batch delete groups", err), nil
		}

		return mcp.NewToolResultText("**Batch delete completed.**"), nil
	}
}
