package tools

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// minProgressInterval throttles progress notifications so a chatty operation
// can't flood the client.
const minProgressInterval = 200 * time.Millisecond

// progressReporter emits notifications/progress frames for a long-running tool
// call, but only when the client supplied a progressToken in the request _meta.
// All methods are no-ops otherwise, so handlers can call them unconditionally.
type progressReporter struct {
	ctx   context.Context
	srv   *server.MCPServer
	token any
	last  time.Time
}

// newProgressReporter builds a reporter bound to the active request. It returns
// a usable (no-op) reporter even when there is no server in context or the
// client omitted a progress token.
func newProgressReporter(ctx context.Context, req mcp.CallToolRequest) *progressReporter {
	p := &progressReporter{ctx: ctx, srv: server.ServerFromContext(ctx)}
	if req.Params.Meta != nil {
		p.token = req.Params.Meta.ProgressToken
	}
	return p
}

func (p *progressReporter) active() bool {
	return p != nil && p.srv != nil && p.token != nil
}

// report sends a progress frame. progress should advance towards total; message
// is a short human-readable status. Emissions are rate-limited except for the
// final frame (progress >= total), which always sends.
func (p *progressReporter) report(progress, total float64, message string) {
	if !p.active() {
		return
	}
	now := time.Now()
	if progress < total && now.Sub(p.last) < minProgressInterval {
		return
	}
	p.last = now
	_ = p.srv.SendNotificationToClient(p.ctx, "notifications/progress", map[string]any{
		"progressToken": p.token,
		"progress":      progress,
		"total":         total,
		"message":       message,
	})
}
