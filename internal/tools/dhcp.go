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

// RegisterDHCP registers DHCP lease management tools.
func RegisterDHCP(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_dhcp_leases",
		mcp.WithTitleAnnotation("List DHCP Leases"),
		mcp.WithDescription("Active DHCP leases: IP, hostname, MAC address, and expiry. Empty if Pi-hole's DHCP server is disabled."),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), dhcpLeasesHandler(r))

	addTool(s, r, mcp.NewTool("pihole_dhcp_delete_lease",
		mcp.WithTitleAnnotation("Delete DHCP Lease"),
		mcp.WithDescription("Remove an active DHCP lease by IP address. Only works when Pi-hole's DHCP server is enabled."),
		mcp.WithString("ip", mcp.Required(), mcp.Description("IP address of the lease to remove.")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), dhcpDeleteLeaseHandler(r))
}

func dhcpLeasesHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.DHCPLeasesResponse
		if err := c.Get(ctx, "/dhcp/leases", &result); err != nil {
			return toolError("get DHCP leases", err), nil
		}

		if len(result.Leases) == 0 {
			return mcp.NewToolResultText("No active DHCP leases (DHCP server may be disabled)."), nil
		}

		if wantCSV(req) {
			headers := []string{"IP", "Hostname", "MAC", "Expires"}
			rows := make([][]string, 0, len(result.Leases))
			for _, l := range result.Leases {
				expiry := "never"
				if l.Expires > 0 {
					expiry = format.Timestamp(float64(l.Expires))
				}
				rows = append(rows, []string{l.IP, l.Name, l.HWAddr, expiry})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d leases:**\n", len(result.Leases))
		for _, l := range result.Leases {
			expiry := "never"
			if l.Expires > 0 {
				expiry = format.Timestamp(float64(l.Expires))
			}
			fmt.Fprintf(&b, "- %s — %s (%s) expires %s\n", l.IP, l.Name, l.HWAddr, expiry)
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func dhcpDeleteLeaseHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ip, err := req.RequireString("ip")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'ip' is required"), nil
		}

		if err := c.Delete(ctx, "/dhcp/leases/"+ip); err != nil {
			return toolError("delete DHCP lease", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Deleted** lease for %s.", ip)), nil
	}
}
