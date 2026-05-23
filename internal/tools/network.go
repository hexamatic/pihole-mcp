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

// RegisterNetwork registers network information tools.
func RegisterNetwork(s *server.MCPServer, c *pihole.Client) {
	addTool(s, mcp.NewTool("pihole_network_devices",
		mcp.WithDescription("Devices seen on the network: MAC, IPs, hostnames, vendor, query count, and first/last seen timestamps. Returns 20 by default."),
		mcp.WithNumber("max_devices", mcp.Description("Max devices (default 20).")),
		mcp.WithNumber("max_addresses", mcp.Description("Max IPs per device (default 3).")),
		detailParam,
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), networkDevicesHandler(c))

	addTool(s, mcp.NewTool("pihole_network_gateway",
		mcp.WithDescription("Network gateway details: address, interface, address family, and local interface IPs."),
		mcp.WithReadOnlyHintAnnotation(true),
	), networkGatewayHandler(c))

	addTool(s, mcp.NewTool("pihole_network_info",
		mcp.WithDescription("Lightweight summary of routing table and interface state. For detailed per-route or per-interface data, use pihole_network_routes or pihole_network_interfaces."),
		mcp.WithReadOnlyHintAnnotation(true),
	), networkInfoHandler(c))

	addTool(s, mcp.NewTool("pihole_network_routes",
		mcp.WithDescription("System routing table entries with family, scope, source, destination, gateway, and outbound interface."),
		mcp.WithReadOnlyHintAnnotation(true),
	), networkRoutesHandler(c))

	addTool(s, mcp.NewTool("pihole_network_interfaces",
		mcp.WithDescription("Network interfaces with addresses, link state, speed, and traffic counters."),
		mcp.WithReadOnlyHintAnnotation(true),
	), networkInterfacesHandler(c))

	addTool(s, mcp.NewTool("pihole_network_delete_device",
		mcp.WithDescription("Permanently delete a network device record by ID. Use pihole_network_devices first to find the ID. Devices may reappear if active on the network."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Numeric device ID from pihole_network_devices output.")),
		mcp.WithDestructiveHintAnnotation(true),
	), networkDeleteDeviceHandler(c))
}

func networkDevicesHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		maxDev := int(req.GetFloat("max_devices", 20))
		maxAddr := int(req.GetFloat("max_addresses", 3))

		path := fmt.Sprintf("/network/devices?max_devices=%d&max_addresses=%d", maxDev, maxAddr)
		var result pihole.NetworkDevicesResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get network devices", err), nil
		}

		if len(result.Devices) == 0 {
			return mcp.NewToolResultText("No network devices found."), nil
		}

		detail := getDetail(req)

		if detail == "minimal" {
			return mcp.NewToolResultText(fmt.Sprintf("%d network devices.", len(result.Devices))), nil
		}

		if wantCSV(req) {
			headers := []string{"ID", "MAC", "Vendor", "IPs", "Queries", "LastQuery"}
			rows := make([][]string, 0, len(result.Devices))
			for _, d := range result.Devices {
				ips := make([]string, 0, len(d.IPs))
				for _, ip := range d.IPs {
					ips = append(ips, ip.IP)
				}
				rows = append(rows, []string{fmt.Sprintf("%d", d.ID), d.HWAddr, format.StringOr(d.MacVendor, ""), strings.Join(ips, ";"), format.Number(d.NumQueries), format.Timestamp(float64(d.LastQuery))})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d devices:**\n", len(result.Devices))
		for _, d := range result.Devices {
			vendor := format.StringOr(d.MacVendor, "unknown")
			ips := make([]string, 0, len(d.IPs))
			for _, ip := range d.IPs {
				name := format.StringOr(ip.Name, "")
				if name != "" {
					ips = append(ips, fmt.Sprintf("%s (%s)", ip.IP, name))
				} else {
					ips = append(ips, ip.IP)
				}
			}
			fmt.Fprintf(&b, "- [id=%d] %s [%s] — %s, %s queries, last %s\n",
				d.ID, d.HWAddr, vendor, strings.Join(ips, ", "),
				format.Number(d.NumQueries), format.Timestamp(float64(d.LastQuery)))
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func networkGatewayHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var result pihole.GatewayResponse
		if err := c.Get(ctx, "/network/gateway", &result); err != nil {
			return toolError("get gateway", err), nil
		}

		if len(result.Gateway) == 0 {
			return mcp.NewToolResultText("No gateway information available."), nil
		}

		var b strings.Builder
		b.WriteString("**Gateway:**\n")
		for _, g := range result.Gateway {
			fmt.Fprintf(&b, "- %s via %s on %s (local: %s)\n",
				g.Address, g.Family, g.Interface, strings.Join(g.Local, ", "))
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func networkInfoHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var routes pihole.NetworkRoutesResponse
		var interfaces pihole.NetworkInterfacesResponse

		_ = c.Get(ctx, "/network/routes", &routes)
		_ = c.Get(ctx, "/network/interfaces", &interfaces)

		var b strings.Builder

		if len(routes.Routes) > 0 {
			fmt.Fprintf(&b, "**%d routes:**\n", len(routes.Routes))
			for _, r := range routes.Routes {
				fmt.Fprintf(&b, "- %s via %s (%s)\n",
					orDefault(r.Dst, "default"),
					orDefault(r.Gateway, "direct"),
					orDefault(r.Dev, r.OIF))
			}
		}

		if len(interfaces.Interfaces) > 0 {
			fmt.Fprintf(&b, "**%d interfaces:**\n", len(interfaces.Interfaces))
			for _, i := range interfaces.Interfaces {
				fmt.Fprintf(&b, "- %s (%s, %s)\n",
					orDefault(i.Name, "?"),
					orDefault(i.Type, "?"),
					orDefault(i.State, "?"))
			}
		}

		if b.Len() == 0 {
			return mcp.NewToolResultText("No network info available."), nil
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func networkRoutesHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var result pihole.NetworkRoutesResponse
		if err := c.Get(ctx, "/network/routes", &result); err != nil {
			return toolError("get routes", err), nil
		}

		if len(result.Routes) == 0 {
			return mcp.NewToolResultText("No routes available."), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d routes:**\n", len(result.Routes))
		for _, r := range result.Routes {
			dst := orDefault(r.Dst, "default")
			gw := orDefault(r.Gateway, "direct")
			dev := orDefault(r.Dev, orDefault(r.OIF, "?"))
			scope := orDefault(r.Scope, "")
			line := fmt.Sprintf("- %s via %s on %s [%s]", dst, gw, dev, r.Family)
			if scope != "" {
				line += fmt.Sprintf(" scope %s", scope)
			}
			if r.Src != "" {
				line += fmt.Sprintf(" src %s", r.Src)
			}
			b.WriteString(line + "\n")
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func networkInterfacesHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var result pihole.NetworkInterfacesResponse
		if err := c.Get(ctx, "/network/interfaces", &result); err != nil {
			return toolError("get interfaces", err), nil
		}

		if len(result.Interfaces) == 0 {
			return mcp.NewToolResultText("No interfaces available."), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d interfaces:**\n", len(result.Interfaces))
		for _, i := range result.Interfaces {
			fmt.Fprintf(&b, "- **%s** (%s)", orDefault(i.Name, "?"), orDefault(i.State, "?"))
			if i.Type != "" {
				fmt.Fprintf(&b, " [%s]", i.Type)
			}
			if i.Speed != nil && *i.Speed > 0 {
				fmt.Fprintf(&b, " speed=%dMb/s", *i.Speed)
			}
			b.WriteString("\n")
			for _, addr := range i.Addresses {
				if addr.PrefixLen > 0 {
					fmt.Fprintf(&b, "  - %s/%d (%s)\n", addr.Address, addr.PrefixLen, addr.Family)
				} else {
					fmt.Fprintf(&b, "  - %s (%s)\n", addr.Address, addr.Family)
				}
			}
			if i.Stats != nil {
				rx := formatStatValue(i.Stats.RxBytes)
				tx := formatStatValue(i.Stats.TxBytes)
				if rx != "" || tx != "" {
					fmt.Fprintf(&b, "  - rx %s / tx %s\n", rx, tx)
				}
			}
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

// formatStatValue formats a {unit, value} envelope from the network_interfaces
// stats response. Empty unit + zero value is rendered as "0B" so callers
// always get a non-empty string to print.
func formatStatValue(v *pihole.NetworkInterfaceValue) string {
	if v == nil {
		return ""
	}
	if v.Unit == "" {
		return format.Bytes(v.Value)
	}
	return fmt.Sprintf("%.2f%sB", v.Value, v.Unit)
}

func networkDeleteDeviceHandler(c *pihole.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := int(req.GetFloat("id", 0))
		if id <= 0 {
			return mcp.NewToolResultError("Parameter 'id' is required and must be a positive integer. Use pihole_network_devices to find the ID."), nil
		}

		if err := c.Delete(ctx, fmt.Sprintf("/network/devices/%d", id)); err != nil {
			return toolError("delete network device", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("**Device %d deleted.** It may reappear if it is still active on the network.", id)), nil
	}
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
