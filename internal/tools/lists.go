package tools

import (
	"context"
	"fmt"
	"net/url"
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
		mcp.WithDescription("List configured blocklists and allowlists with domain counts and update status. Filter by type (allow/block), page with limit/offset."),
		mcp.WithString("type", mcp.Description("Filter: 'allow' or 'block'."), mcp.Enum("allow", "block")),
		limitParam,
		offsetParam,
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
		// Validate paging before the request: a bad parameter is a bad
		// parameter whether or not the collection turns out to be empty,
		// and an empty list is not an answer to limit=-5.
		limit, offset, err := getPage(req, maxPageLimit)
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

		total := len(result.Lists)
		if detail == "minimal" {
			return mcp.NewToolResultText(fmt.Sprintf("%d lists.", total)), nil
		}

		start, end := pageBounds(total, limit, offset)
		page := result.Lists[start:end]

		if wantCSV(req) {
			headers := []string{"Address", "Type", "Domains", "Enabled", "Comment"}
			rows := make([][]string, 0, len(page))
			for _, l := range page {
				rows = append(rows, []string{l.Address, l.Type, format.Number(l.Number), format.Bool(l.Enabled), l.Comment})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows) + format.Truncate(len(page), total)), nil
		}

		var b strings.Builder
		b.WriteString(pageHeading("lists", len(page), total))
		for _, l := range page {
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

		body, err := crudAddBody(req, "address", address)
		if err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		path := "/lists?type=" + url.QueryEscape(t)
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

		path := "/lists/" + pihole.EscapePathSegment(address) + "?type=" + url.QueryEscape(t)

		// The PUT replaces comment and enabled together, so whichever the
		// caller left out has to come from the entry as it stands.
		current := newEntryFields()
		if needsCurrentEntry(req) {
			var existing pihole.ListsResponse
			if err := c.Get(ctx, path, &existing); err != nil {
				return toolError("read the list before updating it", err), nil
			}
			if len(existing.Lists) > 0 {
				current = entryFields{comment: existing.Lists[0].Comment, enabled: existing.Lists[0].Enabled}
			}
		}

		body, err := crudUpdateBody(req, current)
		if err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		body["type"] = t

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

		path := "/lists/" + pihole.EscapePathSegment(address) + "?type=" + url.QueryEscape(t)
		if err := c.Delete(ctx, path); err != nil {
			return toolError("delete list", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Deleted** list: %s.", address)), nil
	}
}

func listsBatchDeleteHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return batchDeleteHandler(r, "lists")
}
