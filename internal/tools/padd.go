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

// RegisterPADD registers the consolidated dashboard tool.
func RegisterPADD(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_padd",
		mcp.WithTitleAnnotation("Dashboard Snapshot"),
		mcp.WithDescription("One-call dashboard: queries, blocking, top domain/client, cache, versions, and host health. Best starting point for a status overview."),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithOutputSchema[PADDOutput](),
	), paddHandler(r))
}

func paddHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		detail := getDetail(req)

		// `full=true` asks Pi-hole for the extended payload (config block,
		// extra interface detail). Only fetch it when the caller wants full
		// detail, keeping the default response compact.
		path := "/padd"
		if detail == "full" {
			path = "/padd?full=true"
		}

		var p pihole.PADDResponse
		if err := c.Get(ctx, path, &p); err != nil {
			return toolError("get dashboard", err), nil
		}

		output := PADDOutput{
			Blocking:       p.Blocking,
			TotalQueries:   p.Queries.Total,
			BlockedQueries: p.Queries.Blocked,
			PercentBlocked: p.Queries.PercentBlocked,
			QueryFrequency: p.Queries.QueryFrequency,
			ActiveClients:  p.ActiveClients,
			GravitySize:    p.GravitySize,
			TopDomain:      deref(p.TopDomain),
			TopBlocked:     deref(p.TopBlocked),
			TopClient:      deref(p.TopClient),
			RecentBlocked:  deref(p.RecentBlocked),
			CacheSize:      p.Cache.Size,
			CPUPercent:     p.CPUPercent,
			MemPercent:     p.MemPercent,
			CoreVersion:    p.Version.Core.Local.Version,
			FTLVersion:     p.Version.FTL.Local.Version,
			WebVersion:     p.Version.Web.Local.Version,
			NodeName:       p.NodeName,
		}

		if detail == "minimal" {
			text := fmt.Sprintf(
				"Blocking: %s | Queries: %s | Blocked: %s (%s) | Clients: %d | Gravity: %s",
				p.Blocking, format.Number(p.Queries.Total), format.Number(p.Queries.Blocked),
				format.Percent(p.Queries.PercentBlocked), p.ActiveClients, format.Number(p.GravitySize))
			return mcp.NewToolResultStructured(output, text), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**Blocking:** %s\n", p.Blocking)
		fmt.Fprintf(&b, "**Queries:** %s total, %s blocked (%s), %.1f/s\n",
			format.Number(p.Queries.Total), format.Number(p.Queries.Blocked),
			format.Percent(p.Queries.PercentBlocked), p.Queries.QueryFrequency)
		fmt.Fprintf(&b, "**Clients:** %d active | **Gravity:** %s domains\n",
			p.ActiveClients, format.Number(p.GravitySize))

		if t := deref(p.TopDomain); t != "" {
			fmt.Fprintf(&b, "**Top domain:** %s\n", t)
		}
		if t := deref(p.TopBlocked); t != "" {
			fmt.Fprintf(&b, "**Top blocked:** %s\n", t)
		}
		if t := deref(p.TopClient); t != "" {
			fmt.Fprintf(&b, "**Top client:** %s\n", t)
		}
		if t := deref(p.RecentBlocked); t != "" {
			fmt.Fprintf(&b, "**Recently blocked:** %s\n", t)
		}

		fmt.Fprintf(&b, "**Cache:** %s entries (%s inserted, %s evicted)\n",
			format.Number(p.Cache.Size), format.Number(p.Cache.Inserted), format.Number(p.Cache.Evicted))

		// Host line: name + FTL CPU/mem, plus CPU temperature when available.
		fmt.Fprintf(&b, "**Host:** %s — FTL CPU %.1f%%, mem %.1f%%",
			format.ValueOr(p.NodeName, "unknown"), p.CPUPercent, p.MemPercent)
		if p.Sensors.CPUTemp != nil {
			fmt.Fprintf(&b, ", CPU temp %.1f%s", *p.Sensors.CPUTemp, format.ValueOr(p.Sensors.Unit, "C"))
		}
		b.WriteString("\n")

		fmt.Fprintf(&b, "**Versions:** core %s, FTL %s, web %s\n",
			format.ValueOr(p.Version.Core.Local.Version, "?"),
			format.ValueOr(p.Version.FTL.Local.Version, "?"),
			format.ValueOr(p.Version.Web.Local.Version, "?"))

		if detail == "full" {
			if p.Iface.V4 != nil {
				writePADDInterface(&b, "IPv4", p.Iface.V4)
			}
			if p.Iface.V6 != nil {
				writePADDInterface(&b, "IPv6", p.Iface.V6)
			}
			if model := deref(p.HostModel); model != "" {
				fmt.Fprintf(&b, "**Model:** %s\n", model)
			}
			if p.PID > 0 {
				fmt.Fprintf(&b, "**FTL PID:** %s\n", format.Number(p.PID))
			}
		}

		return mcp.NewToolResultStructured(output, b.String()), nil
	}
}

func writePADDInterface(b *strings.Builder, label string, iface *pihole.PADDInterface) {
	fmt.Fprintf(b, "**%s (%s):** %s", label, format.ValueOr(iface.Name, "?"), format.ValueOr(deref(iface.Addr), "none"))
	if gw := deref(iface.GWAddr); gw != "" {
		fmt.Fprintf(b, ", gateway %s", gw)
	}
	if iface.RxBytes != nil && iface.TxBytes != nil {
		fmt.Fprintf(b, ", rx %s / tx %s",
			format.SizeWithUnit(iface.RxBytes.Value, iface.RxBytes.Unit),
			format.SizeWithUnit(iface.TxBytes.Value, iface.TxBytes.Unit))
	}
	b.WriteString("\n")
}

// deref returns the string value of a pointer, or "" if nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
