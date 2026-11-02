package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
)

// groupsParam is the groups parameter of the client, domain and list add and
// update tools. No tool could set which groups an entry is in, though the
// client tools said the group tools did and the audit_network prompt
// recommended it; Pi-hole stores membership on the entry, and its group
// endpoints cannot change it.
func groupsParam() mcp.ToolOption {
	return mcp.WithString("groups", mcp.Description(
		`Comma-separated names of the groups to put the entry in, replacing the groups it is in now, e.g. "Default,Kids". Omit to leave them as they are; a new entry joins Default.`))
}

// applyGroups resolves the groups argument, when the caller gave one, to the
// group IDs Pi-hole stores and sets them in body. FTL replaces an entry's
// groups with the array it is sent, and leaves them alone when there is none
// (src/api/list.c). IDs are looked up on this Pi-hole, since they differ from
// one to another.
func applyGroups(ctx context.Context, c *pihole.Client, req mcp.CallToolRequest, body map[string]any) *mcp.CallToolResult {
	raw, given := suppliedString(req, "groups")
	if !given {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, n := range strings.Split(raw, ",") {
		if n = strings.TrimSpace(n); n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return mcp.NewToolResultError("groups names no group, so nothing was changed. An entry in no group applies to no " +
			"client; name at least one, such as Default.")
	}

	var resp pihole.GroupsResponse
	if err := c.Get(ctx, "/groups", &resp); err != nil {
		return toolError("read the groups to assign", err)
	}
	byName := make(map[string]int, len(resp.Groups))
	for _, g := range resp.Groups {
		byName[g.Name] = g.ID
	}
	ids := make([]int, 0, len(names))
	var missing []string
	for _, n := range names {
		id, ok := byName[n]
		if !ok {
			for name, gid := range byName {
				if strings.EqualFold(name, n) {
					id, ok = gid, true
					break
				}
			}
		}
		if !ok {
			missing = append(missing, n)
			continue
		}
		ids = append(ids, id)
	}
	if len(missing) > 0 {
		existing := make([]string, 0, len(byName))
		for name := range byName {
			existing = append(existing, name)
		}
		sort.Strings(existing)
		return mcp.NewToolResultError(fmt.Sprintf("This Pi-hole has no group named %s, so nothing was changed. Its groups are: %s. "+
			"Create one with pihole_groups_add.", strings.Join(missing, ", "), strings.Join(existing, ", ")))
	}
	body["groups"] = ids
	return nil
}

// groupNamer returns a function that renders group IDs as names, from one
// read of this Pi-hole's groups. The list tools printed the IDs, which mean
// nothing to a reader and differ between Pi-holes. If the groups cannot be
// read, the IDs are printed as before.
func groupNamer(ctx context.Context, c *pihole.Client) func([]int) string {
	var resp pihole.GroupsResponse
	names := map[int]string{}
	if err := c.Get(ctx, "/groups", &resp); err == nil {
		for _, g := range resp.Groups {
			names[g.ID] = g.Name
		}
	}
	return func(ids []int) string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if n, ok := names[id]; ok {
				out = append(out, n)
			} else {
				out = append(out, fmt.Sprintf("#%d", id))
			}
		}
		return strings.Join(out, ", ")
	}
}
