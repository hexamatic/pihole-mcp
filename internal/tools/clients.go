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
		mcp.WithDescription("List configured clients with their group assignments, paged with limit/offset. Clients can be identified by IP, MAC, hostname, subnet, or interface."),
		mcp.WithString("client", mcp.Description("Specific client to look up (IP, MAC, hostname).")),
		limitParam,
		offsetParam,
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
		groupsParam(),
		mcp.WithOpenWorldHintAnnotation(true),
	), clientsAddHandler(r))

	addTool(s, r, mcp.NewTool("pihole_clients_update",
		mcp.WithTitleAnnotation("Update Client"),
		mcp.WithDescription("Update a configured client's comment or the groups it belongs to. Fields left out keep their current values."),
		mcp.WithString("client", mcp.Required(), mcp.Description("Client identifier.")),
		mcp.WithString("comment", mcp.Description("Updated comment.")),
		groupsParam(),
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
		mcp.WithDescription("Remove multiple configured clients at once. Each item needs the client identifier."),
		mcp.WithString("items", mcp.Required(), mcp.Description("JSON array: [{\"item\":\"192.168.1.10\"}]")),
		mcp.WithDestructiveHintAnnotation(true),
	), clientsBatchDeleteHandler(r))
}

func clientsListHandler(r *pihole.Registry) server.ToolHandlerFunc {
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
		path := "/clients"
		if client := req.GetString("client", ""); client != "" {
			path += "/" + pihole.EscapePathSegment(client)
		}

		var result pihole.ClientsResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("list clients", err), nil
		}

		if len(result.Clients) == 0 {
			return mcp.NewToolResultText("No configured clients."), nil
		}

		total := len(result.Clients)
		start, end := pageBounds(total, limit, offset)
		page := result.Clients[start:end]

		groupNames := groupNamer(ctx, c)
		if wantCSV(req) {
			headers := []string{"Client", "Name", "Comment", "Groups"}
			rows := make([][]string, 0, len(page))
			for _, cl := range page {
				rows = append(rows, []string{cl.Client, cl.Name, cl.Comment, groupNames(cl.Groups)})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows) + format.Truncate(len(page), total)), nil
		}

		var b strings.Builder
		b.WriteString(pageHeading("clients", len(page), total))
		for _, cl := range page {
			fmt.Fprintf(&b, "- %s", format.Untrusted(cl.Client))
			if cl.Name != "" {
				fmt.Fprintf(&b, " (%s)", format.Untrusted(cl.Name))
			}
			if cl.Comment != "" {
				fmt.Fprintf(&b, " — %s", format.Untrusted(cl.Comment))
			}
			fmt.Fprintf(&b, " [groups: %s]\n", groupNames(cl.Groups))
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

			fmt.Fprintf(&b, "- %s", format.Untrusted(mac))
			if vendor != "" {
				fmt.Fprintf(&b, " (%s)", format.Untrusted(vendor))
			}
			fmt.Fprintf(&b, " — %s", format.Untrusted(ips))
			if names != "" {
				fmt.Fprintf(&b, " [%s]", format.Untrusted(names))
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
		vals, err := requireStrings(req, "client")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client := vals[0]

		if err := validateMaxLength("client", client, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		// A client row has no enabled column, so its body is the identifier
		// and the comment. Group membership survives a write that omits it.
		comment := req.GetString("comment", "")
		if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		body := map[string]any{"client": client, "comment": comment}
		if res := applyGroups(ctx, c, req, body); res != nil {
			return res, nil
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
		vals, err := requireStrings(req, "client")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client := vals[0]

		if err := validateMaxLength("client", client, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		path := "/clients/" + pihole.EscapePathSegment(client)

		// FTL replaces the comment on every PUT, and a comment is the only
		// mutable field a client row has, so an update that does not name one
		// erases it. Read it back rather than write over it. Group membership
		// survives a PUT that omits it, so it needs no round trip.
		unlock := lockEntry(c, path)
		defer func() { unlock() }()
		var existing pihole.ClientsResponse
		if err := c.Get(ctx, path, &existing); err != nil {
			return toolError("read the client before updating it", err), nil
		}
		if len(existing.Clients) == 0 {
			// FTL finds a client by its exact spelling, so a MAC address
			// in the other case found nothing, and the write before this
			// read added a second client for the same device. Look for it
			// ignoring case, and update it under its own lock.
			unlock()
			stored, res := clientNamedIgnoringCase(ctx, c, client)
			if res != nil {
				unlock = func() {}
				return res, nil
			}
			client, path = stored, "/clients/"+pihole.EscapePathSegment(stored)
			unlock = lockEntry(c, path)
			if err := c.Get(ctx, path, &existing); err != nil {
				return toolError("read the client before updating it", err), nil
			}
			if len(existing.Clients) == 0 {
				return missingEntry("client "+client, "pihole_clients_add"), nil
			}
		}
		comment, supplied := suppliedString(req, "comment")
		if !supplied {
			comment = existing.Clients[0].Comment
		}
		if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		body := map[string]any{"comment": comment}
		if res := applyGroups(ctx, c, req, body); res != nil {
			return res, nil
		}

		var result pihole.ClientsResponse
		if err := c.Put(ctx, path, body, &result); err != nil {
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
		vals, err := requireStrings(req, "client")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client := vals[0]

		if err := validateMaxLength("client", client, maxNameLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}

		if err := c.Delete(ctx, "/clients/"+pihole.EscapePathSegment(client)); err != nil {
			return toolError("delete client", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Deleted** client %s.", client)), nil
	}
}

func clientsBatchDeleteHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return batchDeleteHandler(r, "clients")
}

// clientNamedIgnoringCase returns the stored spelling of the one configured
// client whose name matches client ignoring case. It returns a result for the
// caller to send back when there is no such client, or more than one.
func clientNamedIgnoringCase(ctx context.Context, c *pihole.Client, client string) (string, *mcp.CallToolResult) {
	var all pihole.ClientsResponse
	if err := c.Get(ctx, "/clients", &all); err != nil {
		return "", toolError("look the client up", err)
	}
	var found []string
	for _, e := range all.Clients {
		if strings.EqualFold(e.Client, client) {
			found = append(found, e.Client)
		}
	}
	switch len(found) {
	case 0:
		return "", missingEntry("client "+client, "pihole_clients_add")
	case 1:
		return found[0], nil
	default:
		return "", mcp.NewToolResultError(fmt.Sprintf("More than one client matches %s ignoring case (%s), so nothing was changed. "+
			"Name the one to update exactly as pihole_clients_list shows it.", client, strings.Join(found, ", ")))
	}
}
