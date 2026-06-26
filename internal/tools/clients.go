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

// RegisterClients registers client management tools.
func RegisterClients(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_clients_list",
		mcp.WithTitleAnnotation("List Clients"),
		mcp.WithDescription("List configured clients with their group assignments. Clients can be identified by IP, MAC, hostname, subnet, or interface."),
		mcp.WithString("client", mcp.Description("Specific client to look up (IP, MAC, hostname).")),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), clientsListHandler(r))

	addTool(s, r, mcp.NewTool("pihole_clients_suggestions",
		mcp.WithTitleAnnotation("Suggest Clients"),
		mcp.WithDescription("Unconfigured network clients seen by Pi-hole that don't have group assignments yet. Useful for discovering new devices."),
		mcp.WithReadOnlyHintAnnotation(true),
	), clientsSuggestionsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_clients_add",
		mcp.WithTitleAnnotation("Add Client"),
		mcp.WithDescription("Add a client by IP, MAC, hostname, CIDR subnet, or interface name (prefixed with colon, e.g. :eth0)."),
		mcp.WithString("client", mcp.Required(), mcp.Description("Client identifier.")),
		mcp.WithString("comment", mcp.Description("Optional comment.")),
		mcp.WithOpenWorldHintAnnotation(true),
	), clientsAddHandler(r))

	addTool(s, r, mcp.NewTool("pihole_clients_update",
		mcp.WithTitleAnnotation("Update Client"),
		mcp.WithDescription("Update a configured client's comment or group assignments."),
		mcp.WithString("client", mcp.Required(), mcp.Description("Client identifier.")),
		mcp.WithString("comment", mcp.Description("Updated comment.")),
		mcp.WithIdempotentHintAnnotation(true),
	), clientsUpdateHandler(r))

	addTool(s, r, mcp.NewTool("pihole_clients_delete",
		mcp.WithTitleAnnotation("Delete Client"),
		mcp.WithDescription("Remove a configured client. The device remains on the network but loses group-based blocking rules."),
		mcp.WithString("client", mcp.Required(), mcp.Description("Client identifier to remove.")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), clientsDeleteHandler(r))

	addTool(s, r, mcp.NewTool("pihole_clients_batch_delete",
		mcp.WithTitleAnnotation("Batch Delete Clients"),
		mcp.WithDescription("Remove multiple configured clients at once. Provide a JSON array of client identifiers."),
		mcp.WithString("items", mcp.Required(), mcp.Description("JSON array of client identifiers, e.g. [\"192.168.1.10\",\"192.168.1.20\"]")),
		mcp.WithDestructiveHintAnnotation(true),
	), clientsBatchDeleteHandler(r))
}

func clientsListHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path := "/clients"
		if client := req.GetString("client", ""); client != "" {
			path += "/" + client
		}

		var result pihole.ClientsResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("list clients", err), nil
		}

		if len(result.Clients) == 0 {
			return mcp.NewToolResultText("No configured clients."), nil
		}

		if wantCSV(req) {
			headers := []string{"Client", "Name", "Comment", "Groups"}
			rows := make([][]string, 0, len(result.Clients))
			for _, cl := range result.Clients {
				rows = append(rows, []string{cl.Client, cl.Name, cl.Comment, fmt.Sprintf("%v", cl.Groups)})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d clients:**\n", len(result.Clients))
		for _, cl := range result.Clients {
			fmt.Fprintf(&b, "- %s", cl.Client)
			if cl.Name != "" {
				fmt.Fprintf(&b, " (%s)", cl.Name)
			}
			if cl.Comment != "" {
				fmt.Fprintf(&b, " — %s", cl.Comment)
			}
			fmt.Fprintf(&b, " [groups: %v]\n", cl.Groups)
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func clientsSuggestionsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.ClientSuggestionsResponse
		if err := c.Get(ctx, "/clients/_suggestions", &result); err != nil {
			return toolError("get client suggestions", err), nil
		}

		if len(result.Clients) == 0 {
			return mcp.NewToolResultText("No unconfigured clients found."), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d unconfigured clients:**\n", len(result.Clients))
		for _, cl := range result.Clients {
			mac := format.StringOr(cl.HWAddr, "unknown MAC")
			vendor := format.StringOr(cl.MacVendor, "")
			ips := format.StringOr(cl.Addresses, "no IPs")
			names := format.StringOr(cl.Names, "")

			fmt.Fprintf(&b, "- %s", mac)
			if vendor != "" {
				fmt.Fprintf(&b, " (%s)", vendor)
			}
			fmt.Fprintf(&b, " — %s", ips)
			if names != "" {
				fmt.Fprintf(&b, " [%s]", names)
			}
			b.WriteString("\n")
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func clientsAddHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _ := req.RequireString("client")

		if err := validateMaxLength("client", client, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		body := map[string]any{"client": client}
		if comment := req.GetString("comment", ""); comment != "" {
			if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			body["comment"] = comment
		}

		var result pihole.ClientsResponse
		if err := c.Post(ctx, "/clients", body, &result); err != nil {
			return toolError("add client", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Added** client %s.", client)), nil
	}
}

func clientsUpdateHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _ := req.RequireString("client")

		if err := validateMaxLength("client", client, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		body := make(map[string]any)
		if comment := req.GetString("comment", ""); comment != "" {
			if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			body["comment"] = comment
		}

		var result pihole.ClientsResponse
		if err := c.Put(ctx, "/clients/"+client, body, &result); err != nil {
			return toolError("update client", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Updated** client %s.", client)), nil
	}
}

func clientsDeleteHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _ := req.RequireString("client")

		if err := validateMaxLength("client", client, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		if err := c.Delete(ctx, "/clients/"+client); err != nil {
			return toolError("delete client", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Deleted** client %s.", client)), nil
	}
}

func clientsBatchDeleteHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		items, err := req.RequireString("items")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'items' is required (JSON array)"), nil
		}

		if err := c.Post(ctx, "/clients:batchDelete", rawJSON(items), nil); err != nil {
			return toolError("batch delete clients", err), nil
		}

		return mcp.NewToolResultText("**Batch delete completed.**"), nil
	}
}
