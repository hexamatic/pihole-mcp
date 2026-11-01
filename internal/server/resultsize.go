package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// maxToolResultBytes caps the encoded size of one tool result.
//
// It is a backstop, not the way output is kept small. The list tools page and
// the read tools take detail and format for that. What this catches is the
// case nothing else does, such as instance=all across several large Pi-holes,
// before a multi-megabyte result lands in a model's context, where a client
// may truncate it silently or spend the whole window on it.
//
// 512 KiB sits far above anything a normal call returns: a full
// configuration read is about 6 KiB (measured against FTL v6.8).
const maxToolResultBytes = 512 * 1024

// resultSizeLimit replaces a read-only tool's result larger than maxBytes with
// an error that tells the model how to ask for less.
//
// Only read-only tools are capped. The middleware runs after the handler, so
// by the time a result is measured a write tool has already changed Pi-hole.
// Replacing that result would hide what changed: a sync that applied
// thousands of entries would report only a size error and lose its per-entry
// outcome and the path of the backup taken before it ran. A read, by contrast,
// is safe to repeat with narrower arguments, which is what the error says.
//
// The size is measured before the SDK adds protocol metadata (resultType and
// server identity for 2026-07-28 clients), so the bytes on the wire can exceed
// maxBytes by a few hundred. That is fine for a backstop.
//
// mcp-go 1.3 added server.WithToolResultSizeLimit for this, but its
// replacement reads only "tool result exceeds N bytes", which gives a model
// nothing to act on, so the check lives here as server-wide middleware. Being
// server-wide matters: it covers every registration path, including the sync
// tools that bypass addTool. readOnly reports whether the named tool is
// read-only; an unknown tool counts as not read-only and is never capped.
func resultSizeLimit(maxBytes int, readOnly func(name string) bool) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			res, err := next(ctx, req)
			if err != nil || res == nil || maxBytes <= 0 || !readOnly(req.Params.Name) {
				return res, err
			}
			encoded, mErr := json.Marshal(res)
			if mErr != nil || len(encoded) <= maxBytes {
				return res, err
			}
			return mcp.NewToolResultError(fmt.Sprintf(
				"%s returned %d KiB, over this server's %d KiB limit for a single result, so nothing was sent. "+
					"Ask for less: pass limit and offset where the tool takes them, use detail=minimal, "+
					"narrow the time range, or name one instance instead of instance=all.",
				req.Params.Name, len(encoded)/1024, maxBytes/1024)), nil
		}
	}
}
