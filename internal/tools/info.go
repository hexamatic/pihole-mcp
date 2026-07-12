package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterInfo registers system information tools.
func RegisterInfo(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_info_system",
		mcp.WithTitleAnnotation("System Health"),
		mcp.WithDescription("System health: hostname, OS, CPU/memory/disk usage, load averages, temperature, and DNS service status."),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithOutputSchema[InfoSystemOutput](),
	), infoSystemHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_version",
		mcp.WithTitleAnnotation("Component Versions"),
		mcp.WithDescription("Pi-hole component versions: core, FTL engine, web interface, and Docker tag if applicable."),
		mcp.WithReadOnlyHintAnnotation(true),
	), infoVersionHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_database",
		mcp.WithTitleAnnotation("Database Info"),
		mcp.WithDescription("Query database details: file size, total stored queries, and SQLite version."),
		mcp.WithReadOnlyHintAnnotation(true),
	), infoDatabaseHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_messages",
		mcp.WithTitleAnnotation("Diagnostic Messages"),
		mcp.WithDescription("FTL diagnostic messages — warnings about DNS resolution failures, database issues, or configuration problems."),
		mcp.WithReadOnlyHintAnnotation(true),
	), infoMessagesHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_dismiss_message",
		mcp.WithTitleAnnotation("Dismiss Diagnostic Message"),
		mcp.WithDescription("Dismiss an FTL diagnostic message by ID, clearing it from Pi-hole. Get IDs from pihole_info_messages."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Message ID, as reported by pihole_info_messages.")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), infoDismissMessageHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_client",
		mcp.WithTitleAnnotation("Requesting Client Info"),
		mcp.WithDescription("Information about the requesting client's IP address and connection. Does not require authentication."),
		mcp.WithReadOnlyHintAnnotation(true),
	), infoClientHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_ftl",
		mcp.WithTitleAnnotation("FTL Engine Info"),
		mcp.WithDescription("FTL engine process info: PID, privacy level, client and domain counts, and database query total."),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), infoFTLHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_metrics",
		mcp.WithTitleAnnotation("Operational Metrics"),
		mcp.WithDescription("Live DNS and DHCP operational metrics including cache contents, reply counts, and lease statistics."),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), infoMetricsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_sensors",
		mcp.WithTitleAnnotation("Hardware Sensors"),
		mcp.WithDescription("Hardware temperature sensors with names, values, units, and paths."),
		mcp.WithReadOnlyHintAnnotation(true),
	), infoSensorsHandler(r))
}

func infoSystemHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var sysInfo pihole.SystemInfo
		var hostInfo pihole.HostInfo
		var sensors pihole.SensorsInfo

		if err := c.Get(ctx, "/info/system", &sysInfo); err != nil {
			return toolError("get system info", err), nil
		}
		_ = c.Get(ctx, "/info/host", &hostInfo)
		_ = c.Get(ctx, "/info/sensors", &sensors)

		sys := sysInfo.System
		detail := getDetail(req)

		output := InfoSystemOutput{
			Hostname:    hostInfo.Host.Name,
			Uptime:      sys.Uptime,
			LoadAverage: sys.Load[0],
			CPUCores:    sys.CPU.Nprocs,
			CPUPercent:  sys.CPU.Perc,
			MemoryUsed:  sys.Memory.RAM.Used,
			MemoryTotal: sys.Memory.RAM.Total,
			MemoryUnit:  sys.Memory.RAM.Unit,
			MemoryPerc:  sys.Memory.RAM.Perc,
			DiskPercent: sys.Disk.Perc,
			DNSRunning:  sys.DNS.Running,
		}

		if detail == "minimal" {
			mem := format.SizeWithUnit(sys.Memory.RAM.Used, sys.Memory.RAM.Unit)
			dns := "up"
			if !sys.DNS.Running {
				dns = "down"
			}
			text := fmt.Sprintf(
				"Load: %.2f | Memory: %s (%.0f%%) | DNS: %s | Uptime: %s",
				sys.Load[0], mem, sys.Memory.RAM.Perc, dns, format.Duration(float64(sys.Uptime)))
			return mcp.NewToolResultStructured(output, text), nil
		}

		var b strings.Builder
		if hostInfo.Host.Name != "" {
			fmt.Fprintf(&b, "**Host:** %s (%s %s)\n", hostInfo.Host.Name, hostInfo.Host.OS, hostInfo.Host.Arch)
		}
		fmt.Fprintf(&b, "**Uptime:** %s\n", format.Duration(float64(sys.Uptime)))
		fmt.Fprintf(&b, "**Load:** %.2f, %.2f, %.2f\n", sys.Load[0], sys.Load[1], sys.Load[2])
		fmt.Fprintf(&b, "**CPU:** %d cores, %.1f%% used\n", sys.CPU.Nprocs, sys.CPU.Perc)
		fmt.Fprintf(&b, "**Memory:** %s / %s (%.1f%%)\n",
			format.SizeWithUnit(sys.Memory.RAM.Used, sys.Memory.RAM.Unit),
			format.SizeWithUnit(sys.Memory.RAM.Total, sys.Memory.RAM.Unit),
			sys.Memory.RAM.Perc)
		fmt.Fprintf(&b, "**Disk:** %s / %s (%.1f%%)\n",
			format.SizeWithUnit(sys.Disk.Used, sys.Disk.Unit),
			format.SizeWithUnit(sys.Disk.Total, sys.Disk.Unit),
			sys.Disk.Perc)

		if sys.DNS.Running {
			b.WriteString("**DNS:** running\n")
		} else {
			b.WriteString("**DNS:** not running (expected in Docker — Pi-hole manages DNS internally)\n")
		}

		for _, t := range sensors.Sensors.Temperatures {
			fmt.Fprintf(&b, "**%s:** %.1f%s\n", t.Name, t.Value, t.Unit)
		}

		if detail == "full" && hostInfo.Host.Kernel != "" {
			fmt.Fprintf(&b, "**Kernel:** %s\n", hostInfo.Host.Kernel)
			fmt.Fprintf(&b, "**Domain:** %s\n", format.ValueOr(hostInfo.Host.Domain, "N/A"))
		}

		return mcp.NewToolResultStructured(output, b.String()), nil
	}
}

func infoVersionHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var ver pihole.VersionInfo
		if err := c.Get(ctx, "/info/version", &ver); err != nil {
			return toolError("get version", err), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**Core:** %s (%s)\n", ver.Version.Core.Local.Version, ver.Version.Core.Local.Branch)
		fmt.Fprintf(&b, "**FTL:** %s (%s)\n", ver.Version.FTL.Local.Version, ver.Version.FTL.Local.Branch)
		fmt.Fprintf(&b, "**Web:** %s (%s)\n", ver.Version.Web.Local.Version, ver.Version.Web.Local.Branch)
		if ver.Version.Docker.Local != "" {
			fmt.Fprintf(&b, "**Docker:** %s\n", ver.Version.Docker.Local)
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func infoDatabaseHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var db pihole.DatabaseInfo
		if err := c.Get(ctx, "/info/database", &db); err != nil {
			return toolError("get database info", err), nil
		}

		size := format.SizeWithUnit(db.Size, db.Unit)
		sqlite := format.ValueOr(db.SQLite, "N/A")

		return mcp.NewToolResultText(fmt.Sprintf(
			"**Size:** %s | **Queries:** %s | **SQLite:** %s",
			size, format.Number(db.Queries), sqlite)), nil
	}
}

func infoMessagesHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var msgs pihole.MessagesResponse
		if err := c.Get(ctx, "/info/messages", &msgs); err != nil {
			return toolError("get messages", err), nil
		}

		if len(msgs.Messages) == 0 {
			return mcp.NewToolResultText("No diagnostic messages."), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d diagnostic message(s):**\n", len(msgs.Messages))
		for _, m := range msgs.Messages {
			// The ID is what pihole_info_dismiss_message needs, so surface it.
			fmt.Fprintf(&b, "- `id=%d` [%s] %s (%s)\n",
				m.ID, m.Type, m.Plain, format.Timestamp(float64(m.Timestamp)))
		}
		b.WriteString("\nDismiss one with pihole_info_dismiss_message once you have dealt with it.")

		return mcp.NewToolResultText(b.String()), nil
	}
}

func infoDismissMessageHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		id, err := req.RequireInt("id")
		if err != nil {
			return mcp.NewToolResultError("id is required — call pihole_info_messages to list the message IDs"), nil
		}
		if id < 0 {
			return mcp.NewToolResultError(fmt.Sprintf("id must not be negative (got %d)", id)), nil
		}

		if err := c.Delete(ctx, fmt.Sprintf("/info/messages/%d", id)); err != nil {
			return toolError("dismiss message", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Dismissed diagnostic message %d.", id)), nil
	}
}

func infoClientHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var info pihole.ClientInfo
		if err := c.Get(ctx, "/info/client", &info); err != nil {
			return toolError("get client info", err), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf(
			"**Remote address:** %s | **HTTP:** %s | **Method:** %s",
			info.RemoteAddr, info.HTTPVersion, info.Method)), nil
	}
}

func infoFTLHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var ftl pihole.FTLInfo
		if err := c.Get(ctx, "/info/ftl", &ftl); err != nil {
			return toolError("get FTL info", err), nil
		}

		detail := getDetail(req)
		d := ftl.FTL

		if detail == "minimal" {
			return mcp.NewToolResultText(fmt.Sprintf(
				"FTL PID: %s | Privacy: %d | Active clients: %s | Gravity: %s",
				format.Number(d.PID), d.PrivacyLevel,
				format.Number(d.Clients.Active), format.Number(d.Database.Gravity))), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**PID:** %s\n", format.Number(d.PID))
		fmt.Fprintf(&b, "**Uptime:** %.0fs\n", d.Uptime)
		fmt.Fprintf(&b, "**Privacy level:** %d\n", d.PrivacyLevel)
		fmt.Fprintf(&b, "**Query frequency:** %.2f/s\n", d.QueryFrequency)
		fmt.Fprintf(&b, "**Active clients:** %s of %s known\n",
			format.Number(d.Clients.Active), format.Number(d.Clients.Total))
		fmt.Fprintf(&b, "**Gravity domains:** %s across %s lists in %s groups\n",
			format.Number(d.Database.Gravity), format.Number(d.Database.Lists), format.Number(d.Database.Groups))
		fmt.Fprintf(&b, "**Memory:** %.2f%% | **CPU:** %.2f%%\n", d.MemPercent, d.CPUPercent)
		fmt.Fprintf(&b, "**Destructive operations allowed:** %v\n", d.AllowDestructive)

		return mcp.NewToolResultText(b.String()), nil
	}
}

func infoMetricsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var metrics pihole.MetricsInfo
		if err := c.Get(ctx, "/info/metrics", &metrics); err != nil {
			return toolError("get metrics", err), nil
		}

		detail := getDetail(req)

		if detail == "minimal" {
			return mcp.NewToolResultText(fmt.Sprintf(
				"Metrics: %d top-level keys", len(metrics.Metrics))), nil
		}

		if detail == "full" {
			metricsJSON, err := json.MarshalIndent(metrics.Metrics, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to format metrics: %v", err)), nil
			}
			var b strings.Builder
			b.WriteString("```json\n")
			b.Write(metricsJSON)
			b.WriteString("\n```")
			return mcp.NewToolResultText(b.String()), nil
		}

		// normal: key-value pairs
		keys := make([]string, 0, len(metrics.Metrics))
		for k := range metrics.Metrics {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var b strings.Builder
		for _, k := range keys {
			v := metrics.Metrics[k]
			if nested, ok := v.(map[string]any); ok {
				fmt.Fprintf(&b, "**%s:** %d sub-keys\n", k, len(nested))
			} else {
				fmt.Fprintf(&b, "**%s:** %v\n", k, v)
			}
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func infoSensorsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var sensors pihole.SensorsInfo
		if err := c.Get(ctx, "/info/sensors", &sensors); err != nil {
			return toolError("get sensors", err), nil
		}

		if len(sensors.Sensors.Temperatures) == 0 {
			return mcp.NewToolResultText("No sensor data available."), nil
		}

		var b strings.Builder
		for _, t := range sensors.Sensors.Temperatures {
			fmt.Fprintf(&b, "**%s:** %.1f%s\n", t.Name, t.Value, t.Unit)
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}
