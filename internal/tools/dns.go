package tools

import (
	"context"
	"fmt"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterDNS registers DNS blocking control tools.
func RegisterDNS(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r,
		mcp.NewTool("pihole_dns_get_blocking",
			mcp.WithTitleAnnotation("Get Blocking Status"),
			mcp.WithDescription("Get the current DNS blocking status and any active timer for temporary blocking changes."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithOutputSchema[BlockingStatusOutput](),
		),
		dnsGetBlockingHandler(r),
	)

	addTool(s, r,
		mcp.NewTool("pihole_dns_set_blocking",
			mcp.WithTitleAnnotation("Set Blocking"),
			mcp.WithDescription("Enable or disable DNS blocking. Set a timer (seconds) to automatically revert after a duration."),
			mcp.WithBoolean("blocking",
				mcp.Required(),
				mcp.Description("True to enable blocking, false to disable."),
			),
			mcp.WithNumber("timer",
				mcp.Description("Seconds until automatic revert. Omit for permanent change."),
			),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
		),
		dnsSetBlockingHandler(r),
	)
}

func dnsGetBlockingHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var status pihole.BlockingStatus
		if err := c.Get(ctx, "/dns/blocking", &status); err != nil {
			return toolError("get blocking status", err), nil
		}

		output := BlockingStatusOutput{
			Blocking: status.Blocking,
			Timer:    status.Timer,
		}

		text := fmt.Sprintf("**Blocking:** %s", status.Blocking)
		if status.Timer != nil {
			text += fmt.Sprintf(" (%.0fs remaining)", *status.Timer)
		}

		return mcp.NewToolResultStructured(output, text), nil
	}
}

func dnsSetBlockingHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		blocking, err := req.RequireBool("blocking")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'blocking' is required (true or false)"), nil
		}

		body := pihole.BlockingRequest{Blocking: blocking}

		timer := req.GetFloat("timer", 0)
		if timer > 0 {
			body.Timer = &timer
		}

		var status pihole.BlockingStatus
		if err := c.Post(ctx, "/dns/blocking", body, &status); err != nil {
			return toolError("set blocking", err), nil
		}

		action := "enabled"
		if !blocking {
			action = "disabled"
		}
		text := fmt.Sprintf("**Blocking %s.** Status: %s", action, status.Blocking)
		if status.Timer != nil {
			text += fmt.Sprintf(" (reverts in %.0fs)", *status.Timer)
		}

		return mcp.NewToolResultText(text), nil
	}
}
