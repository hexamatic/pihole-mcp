package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// getTimeRange extracts from/until Unix timestamp parameters with a default window.
// If defaultWindow is non-zero, from defaults to (now - defaultWindow) and until defaults to now.
// Returns the formatted string values ready for query parameters.
func getTimeRange(req mcp.CallToolRequest, defaultWindow time.Duration) (from, until string) {
	now := float64(time.Now().Unix())
	var defaultFrom float64
	if defaultWindow > 0 {
		defaultFrom = now - defaultWindow.Seconds()
	}
	f := req.GetFloat("from", defaultFrom)
	u := req.GetFloat("until", now)
	return fmt.Sprintf("%.0f", f), fmt.Sprintf("%.0f", u)
}

// getCountCapped extracts an integer count parameter, capping it at maxVal and
// rejecting anything below minVal.
//
// The two directions are deliberately not symmetric. A count above the cap is a
// request for as much as the tool will give, so capping answers it. A count
// below the floor has no useful reading, and the old code passed it straight
// through: count=-5 asked Pi-hole for -5 rows, got none, and rendered an empty
// list with isError false. That is indistinguishable from a Pi-hole with no
// data, which is the one answer a caller must never be given by mistake.
func getCountCapped(req mcp.CallToolRequest, key string, defaultVal, minVal, maxVal int) (int, error) {
	v := int(req.GetFloat(key, float64(defaultVal)))
	if v < minVal {
		return 0, fmt.Errorf("parameter '%s' must be at least %d (got %d)", key, minVal, v)
	}
	if v > maxVal {
		return maxVal, nil
	}
	return v, nil
}

// getPage extracts the limit/offset pair shared by the list tools. Both are
// optional; limit 0 means "no limit", which is the historical behaviour these
// tools had before the parameters existed.
func getPage(req mcp.CallToolRequest, maxLimit int) (limit, offset int, err error) {
	limit, err = getCountCapped(req, "limit", 0, 0, maxLimit)
	if err != nil {
		return 0, 0, err
	}
	offset, err = getCountCapped(req, "offset", 0, 0, math.MaxInt32)
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

// pageBounds turns a limit/offset pair into slice bounds over total items.
// An offset past the end yields an empty page rather than a panic, so a caller
// paging off the end gets "0 of 247" instead of an error.
func pageBounds(total, limit, offset int) (start, end int) {
	start = min(offset, total)
	end = total
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return start, end
}

// pageHeading renders the count line for a list, naming the total whenever the
// page shown is not the whole collection.
func pageHeading(noun string, shown, total int) string {
	if shown == total {
		return fmt.Sprintf("**%d %s:**\n", total, noun)
	}
	return fmt.Sprintf("**%d of %d %s:**\n", shown, total, noun)
}

// flattenInto walks a decoded JSON tree and appends one "dotted.path: value"
// line per leaf, sorted by key at every level so the same data always renders
// as the same bytes.
//
// This is what a config or metrics reader is actually asking for. Summarising a
// nested map as "29 settings" is a count of the answer rather than the answer,
// and no amount of re-reading it produces an upstream server or a cache hit
// rate.
func flattenInto(out *[]string, prefix string, v any) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			*out = append(*out, prefix+": {}")
			return
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := k
			if prefix != "" {
				child = prefix + "." + k
			}
			flattenInto(out, child, t[k])
		}
	default:
		*out = append(*out, prefix+": "+renderLeaf(v))
	}
}

// renderLeaf formats a non-map value for a flattened line. Strings go through
// bare so an upstream reads as 8.8.8.8#53 rather than "8.8.8.8#53"; everything
// else goes through JSON, which keeps arrays, nulls and large numbers
// unambiguous and never emits Go's scientific notation for a plain integer.
func renderLeaf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	// Everything reaching here came out of a JSON decode, so it is a bool, a
	// number, nil, or a slice or map of those. None of them can fail to
	// marshal, which is why the error is discarded rather than handled.
	b, _ := json.Marshal(v)
	return string(b)
}

// flattenTree renders a whole decoded JSON object as sorted dotted lines.
func flattenTree(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	lines := make([]string, 0, len(m))
	flattenInto(&lines, "", m)
	return strings.Join(lines, "\n") + "\n"
}

// writeProcessedResult writes bulk operation results to a string builder.
func writeProcessedResult(b *strings.Builder, p *pihole.ProcessedResult) {
	if p == nil {
		return
	}
	for _, s := range p.Success {
		fmt.Fprintf(b, "- Added: %s\n", s.Item)
	}
	for _, e := range p.Errors {
		fmt.Fprintf(b, "- Failed: %s (%s)\n", e.Item, e.Error)
	}
}

// ---------------------------------------------------------------------------
// CRUD writes
//
// FTL treats a PUT to a domain, list or group as a full replacement of both
// comment and enabled: a body carrying only a comment re-enables a disabled
// entry, and one carrying only enabled nulls the comment. Both verified
// against FTL v6.7. Group membership is the exception, surviving a PUT that
// omits it, so it needs no round trip. The helpers below are shared by the
// four CRUD tool families so the shape is fixed in one place.
// ---------------------------------------------------------------------------

// entryFields are the two columns a CRUD PUT replaces wholesale. The zero
// value is deliberately not useful: use newEntryFields for the defaults that
// apply when no entry exists yet.
type entryFields struct {
	comment string
	enabled bool
}

// newEntryFields returns the values a write assumes when there is no entry to
// read them from, matching each tool's documented defaults.
func newEntryFields() entryFields { return entryFields{enabled: true} }

// suppliedString reports whether the caller set key, and its value. GetString
// cannot tell an omitted parameter from an empty one, and for an update that
// difference decides whether a comment is edited or erased.
func suppliedString(req mcp.CallToolRequest, key string) (string, bool) {
	v, ok := req.GetArguments()[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// suppliedBool reports whether the caller set key, and its value.
func suppliedBool(req mcp.CallToolRequest, key string) (bool, bool) {
	v, ok := req.GetArguments()[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// needsCurrentEntry reports whether an update has to read the entry before
// writing it. A caller that supplied both fields replaces both, so there is
// nothing to carry over and no round trip to pay for.
func needsCurrentEntry(req mcp.CallToolRequest) bool {
	_, hasComment := suppliedString(req, "comment")
	_, hasEnabled := suppliedBool(req, "enabled")
	return !hasComment || !hasEnabled
}

// crudAddBody builds the create body for a CRUD add tool. comment and enabled
// are always sent so FTL's own defaults never decide them: FTL stores an empty
// comment as NULL, so sending one costs nothing.
func crudAddBody(req mcp.CallToolRequest, key string, value any) (map[string]any, error) {
	comment := req.GetString("comment", "")
	if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
		return nil, err
	}
	return map[string]any{
		key:       value,
		"comment": comment,
		"enabled": req.GetBool("enabled", true),
	}, nil
}

// crudUpdateBody builds the replacement body for a CRUD update tool. Values
// the caller supplied win; the rest come from current, read back beforehand,
// so editing one field leaves the other as it was.
func crudUpdateBody(req mcp.CallToolRequest, current entryFields) (map[string]any, error) {
	body := map[string]any{"comment": current.comment, "enabled": current.enabled}
	if comment, ok := suppliedString(req, "comment"); ok {
		if err := validateMaxLength("comment", comment, maxCommentLength); err != nil {
			return nil, err
		}
		body["comment"] = comment
	}
	if enabled, ok := suppliedBool(req, "enabled"); ok {
		body["enabled"] = enabled
	}
	return body, nil
}

// batchDeleteHandler builds the handler shared by the four batch-delete tools.
// Each posts the caller's pre-encoded JSON array to <collection>:batchDelete.
func batchDeleteHandler(r *pihole.Registry, collection string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		items, err := req.RequireString("items")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'items' is required (JSON array)"), nil
		}

		if err := c.Post(ctx, "/"+collection+":batchDelete", rawJSON(items), nil); err != nil {
			return toolError("batch delete "+collection, err), nil
		}

		return mcp.NewToolResultText("**Batch delete completed.**"), nil
	}
}

// rawJSON passes pre-encoded JSON through json.Marshal unchanged.
type rawJSON string

// MarshalJSON implements the json.Marshaler interface.
func (r rawJSON) MarshalJSON() ([]byte, error) {
	return []byte(r), nil
}

// escapeConfigElement turns a dotted config path such as "dns.upstreams" into
// its URL path form, percent-encoding each component on its own.
//
// Escaping the joined string instead is the trap here: by then the dots are
// separators, and encoding them to %2F 404s every config request.
func escapeConfigElement(element string) string {
	parts := strings.Split(element, ".")
	for i, p := range parts {
		parts[i] = pihole.EscapePathSegment(p)
	}
	return strings.Join(parts, "/")
}
