package tools

import (
	"errors"
	"fmt"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
)

// toolError creates an actionable MCP tool error from a Pi-hole API error.
// It inspects the error type and adds contextual guidance for tool clients
// to understand what went wrong and how to fix it.
func toolError(action string, err error) *mcp.CallToolResult {
	msg := fmt.Sprintf("Failed to %s", action)

	var authErr *pihole.AuthError
	var notFoundErr *pihole.NotFoundError
	var validationErr *pihole.ValidationError
	var rateLimitErr *pihole.RateLimitError
	var apiErr *pihole.APIError

	switch {
	case errors.As(err, &authErr):
		msg += fmt.Sprintf(": %s. Check PIHOLE_PASSWORD or use an application password.", authErr.Message)
	case errors.As(err, &rateLimitErr):
		msg += fmt.Sprintf(": %s. Too many requests — try again shortly.", rateLimitErr.Message)
	case errors.As(err, &notFoundErr):
		msg += fmt.Sprintf(": %s. The requested resource does not exist.", notFoundErr.Message)
	case errors.As(err, &validationErr):
		msg += fmt.Sprintf(": %s", validationErr.Message)
		if validationErr.Hint != "" {
			msg += " (" + validationErr.Hint + ")"
		}
	case errors.As(err, &apiErr):
		msg += fmt.Sprintf(": %s", apiErr.Message)
		if apiErr.Hint != "" {
			msg += " (" + apiErr.Hint + ")"
		}
	default:
		msg += fmt.Sprintf(": %v", err)
	}

	return mcp.NewToolResultError(msg)
}
