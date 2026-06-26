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

// RegisterAuth registers session management tools.
func RegisterAuth(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_auth_sessions",
		mcp.WithTitleAnnotation("List Sessions"),
		mcp.WithDescription("List active API sessions: remote address, user agent, and expiry. Useful for detecting unauthorised access."),
		mcp.WithReadOnlyHintAnnotation(true),
	), authSessionsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_auth_revoke_session",
		mcp.WithTitleAnnotation("Revoke Session"),
		mcp.WithDescription("Revoke an active API session by ID. Cannot revoke the current session used by this server."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Session ID to revoke.")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), authRevokeSessionHandler(r))
}

func authSessionsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.SessionsResponse
		if err := c.Get(ctx, "/auth/sessions", &result); err != nil {
			return toolError("get sessions", err), nil
		}

		if len(result.Sessions) == 0 {
			return mcp.NewToolResultText("No active sessions."), nil
		}

		var b strings.Builder
		for _, s := range result.Sessions {
			current := ""
			if s.CurrentSession {
				current = " (current)"
			}
			fmt.Fprintf(&b, "- Session %d: %s (%s) — expires %s%s\n",
				s.ID, s.RemoteAddr, s.UserAgent,
				format.Timestamp(s.ValidUntil), current)
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func authRevokeSessionHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		id := int(req.GetFloat("id", 0))
		if id <= 0 {
			return mcp.NewToolResultError("Parameter 'id' is required"), nil
		}

		if err := c.Delete(ctx, fmt.Sprintf("/auth/session/%d", id)); err != nil {
			return toolError("revoke session", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Session %d revoked.", id)), nil
	}
}
