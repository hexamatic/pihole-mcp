package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
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
	added := map[int]bool{}
	var missing []string
	for _, n := range names {
		id, err := groupID(byName, n)
		if err != nil {
			return mcp.NewToolResultError(err.Error())
		}
		if id < 0 {
			missing = append(missing, n)
			continue
		}
		// Each group once: FTL clears the entry's groups and then inserts
		// them one by one, and a repeated ID stopped it part way, leaving the
		// entry out of its old groups and out of the rest of the new ones.
		if !added[id] {
			added[id] = true
			ids = append(ids, id)
		}
	}
	if len(missing) > 0 {
		existing := make([]string, 0, len(byName))
		for name := range byName {
			existing = append(existing, name)
		}
		sort.Strings(existing)
		return mcp.NewToolResultError(fmt.Sprintf("This Pi-hole has no group named %s, so nothing was changed. Its groups are: %s. "+
			"Create one with pihole_groups_add.", format.Untrusted(strings.Join(missing, ", ")), format.Untrusted(strings.Join(existing, ", "))))
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
				out = append(out, format.Untrusted(n))
			} else {
				out = append(out, fmt.Sprintf("#%d", id))
			}
		}
		return strings.Join(out, ", ")
	}
}

// groupID finds a group by name: the exact name first, then one that differs
// only in case. It returns -1 when none matches, and an error when the name
// matches several groups by case alone, since Pi-hole allows "Kids" and
// "KIDS" side by side and picking either would be a guess.
func groupID(byName map[string]int, name string) (int, error) {
	if id, ok := byName[name]; ok {
		return id, nil
	}
	var matches []string
	for n := range byName {
		if strings.EqualFold(n, name) {
			matches = append(matches, n)
		}
	}
	switch len(matches) {
	case 0:
		return -1, nil
	case 1:
		return byName[matches[0]], nil
	default:
		sort.Strings(matches)
		return 0, fmt.Errorf("%q matches more than one group (%s), so nothing was changed; give the exact name", name, strings.Join(matches, ", "))
	}
}
