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
		mcp.WithDescription("System health: hostname, OS, uptime, CPU and memory usage, load averages, temperatures, and FTL's own resource use."),
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
		mcp.WithDescription("Query database details: file size, total stored queries, SQLite version, and how far back the stored queries reach."),
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
		mcp.WithDescription("FTL engine process info: PID, uptime, privacy level, client and gravity counts, and at full detail dnsmasq's internal DNS and DHCP counters."),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), infoFTLHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_metrics",
		mcp.WithTitleAnnotation("Operational Metrics"),
		mcp.WithDescription("Live DNS and DHCP operational metrics as dotted key/value lines: cache size, inserts, evictions, reply counts and lease statistics."),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), infoMetricsHandler(r))

	addTool(s, r, mcp.NewTool("pihole_info_sensors",
		mcp.WithTitleAnnotation("Hardware Sensors"),
		mcp.WithDescription("Hardware temperature readings from each sensor, with their warning and critical limits, in the unit Pi-hole is set to show."),
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
		// Host details and temperatures add to the report but are not
		// essential to it, so a failure leaves their lines out.
		_ = c.Get(ctx, "/info/host", &hostInfo)
		_ = c.Get(ctx, "/info/sensors", &sensors)

		sys := sysInfo.System
		host := hostInfo.Host.Uname
		ram := sys.Memory.RAM
		detail := getDetail(req)

		output := InfoSystemOutput{
			Hostname:    host.Nodename,
			Uptime:      sys.Uptime,
			LoadAverage: sys.CPU.Load.Raw[0],
			CPUCores:    sys.CPU.Nprocs,
			CPUPercent:  sys.CPU.PercentUsed,
			MemoryUsed:  ram.Used,
			MemoryTotal: ram.Total,
			MemoryUnit:  "KiB",
			MemoryPerc:  ram.PercentUsed,
		}

		if detail == "minimal" {
			text := fmt.Sprintf(
				"Load: %.2f | CPU: %.0f%% | Memory: %s (%.0f%%) | Uptime: %s",
				sys.CPU.Load.Raw[0], sys.CPU.PercentUsed, kibibytes(ram.Used), ram.PercentUsed,
				format.Duration(float64(sys.Uptime)))
			return mcp.NewToolResultStructured(output, text), nil
		}

		var b strings.Builder
		if host.Nodename != "" {
			fmt.Fprintf(&b, "**Host:** %s (%s %s)\n", host.Nodename, host.Sysname, host.Machine)
		}
		fmt.Fprintf(&b, "**Uptime:** %s\n", format.Duration(float64(sys.Uptime)))
		load := sys.CPU.Load.Raw
		fmt.Fprintf(&b, "**Load:** %.2f, %.2f, %.2f\n", load[0], load[1], load[2])
		fmt.Fprintf(&b, "**CPU:** %d cores, %.1f%% used\n", sys.CPU.Nprocs, sys.CPU.PercentUsed)
		fmt.Fprintf(&b, "**Memory:** %s of %s used (%.1f%%), %s available\n",
			kibibytes(ram.Used), kibibytes(ram.Total), ram.PercentUsed, kibibytes(ram.Available))
		if swap := sys.Memory.Swap; swap.Total > 0 {
			fmt.Fprintf(&b, "**Swap:** %s of %s used (%.1f%%)\n",
				kibibytes(swap.Used), kibibytes(swap.Total), swap.PercentUsed)
		}
		fmt.Fprintf(&b, "**FTL:** %.1f%% CPU, %.1f%% memory\n", sys.FTL.CPU, sys.FTL.Memory)
		if t := sensors.Sensors.CPUTemp; t != nil {
			fmt.Fprintf(&b, "**CPU temperature:** %s", temperature(*t, sensors.Sensors.Unit))
			if limit := sensors.Sensors.HotLimit; limit > 0 {
				fmt.Fprintf(&b, " (warns above %s)", temperature(limit, sensors.Sensors.Unit))
			}
			b.WriteString("\n")
		}

		if detail == "full" {
			if host.Release != "" {
				fmt.Fprintf(&b, "**Kernel:** %s %s\n", host.Sysname, host.Release)
			}
			// uname reports an unset NIS domain as "(none)".
			if d := host.Domainname; d != "" && d != "(none)" {
				fmt.Fprintf(&b, "**Domain:** %s\n", d)
			}
			if m := deref(hostInfo.Host.Model); m != "" {
				fmt.Fprintf(&b, "**Model:** %s\n", m)
			}
			fmt.Fprintf(&b, "**Processes:** %d\n", sys.Procs)
		}

		return mcp.NewToolResultStructured(output, b.String()), nil
	}
}

// kibibytes formats a memory figure FTL reports in KiB.
func kibibytes(kib float64) string {
	return format.Bytes(kib * 1024)
}

// temperature formats a reading in the unit FTL is set to report.
func temperature(v float64, unit string) string {
	switch unit {
	case "K":
		return fmt.Sprintf("%.1f K", v)
	case "":
		return fmt.Sprintf("%.1f°C", v)
	default:
		return fmt.Sprintf("%.1f°%s", v, unit)
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

		var b strings.Builder
		fmt.Fprintf(&b, "**Size:** %s | **Queries:** %s | **SQLite:** %s",
			size, format.Number(db.Queries), sqlite)
		// How far back the database reaches decides whether a historical query
		// search can succeed at all, so report it here rather than leaving the
		// caller to discover it from an empty result.
		if db.EarliestTimestamp > 0 {
			fmt.Fprintf(&b, "\n**Earliest query:** %s", format.Timestamp(db.EarliestTimestamp))
		}
		if db.EarliestTimestampDisk > 0 {
			fmt.Fprintf(&b, "\n**Earliest on disk:** %s (%s queries)",
				format.Timestamp(db.EarliestTimestampDisk), format.Number(db.QueriesDisk))
		}

		return mcp.NewToolResultText(b.String()), nil
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
		// FTL reports its uptime in milliseconds (src/api/info.c); it was
		// printed as seconds, a thousand times too long.
		fmt.Fprintf(&b, "**Uptime:** %s\n", format.Duration(d.Uptime/1000))
		fmt.Fprintf(&b, "**Privacy level:** %d\n", d.PrivacyLevel)
		fmt.Fprintf(&b, "**Query frequency:** %.2f/s\n", d.QueryFrequency)
		fmt.Fprintf(&b, "**Active clients:** %s of %s known\n",
			format.Number(d.Clients.Active), format.Number(d.Clients.Total))
		fmt.Fprintf(&b, "**Gravity domains:** %s across %s lists in %s groups\n",
			format.Number(d.Database.Gravity), format.Number(d.Database.Lists), format.Number(d.Database.Groups))
		fmt.Fprintf(&b, "**Memory:** %.2f%% | **CPU:** %.2f%%\n", d.MemPercent, d.CPUPercent)
		fmt.Fprintf(&b, "**Destructive operations allowed:** %v\n", d.AllowDestructive)

		// dnsmasq's own counters are what this tool offers beyond
		// pihole_info_system, and the server instructions say so; they were
		// decoded and never shown.
		if detail == "full" && len(d.Dnsmasq) > 0 {
			keys := make([]string, 0, len(d.Dnsmasq))
			for k := range d.Dnsmasq {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString("**dnsmasq counters:**\n")
			for _, k := range keys {
				if n, ok := d.Dnsmasq[k].(float64); ok {
					fmt.Fprintf(&b, "- %s: %s\n", k, format.Number(int(n)))
				}
			}
		}

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
			keys := make([]string, 0, len(metrics.Metrics))
			for k := range metrics.Metrics {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return mcp.NewToolResultText(fmt.Sprintf(
				"Metric groups: %s", strings.Join(keys, ", "))), nil
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

		// normal: one dotted line per metric. Every metric FTL reports lives
		// one or two levels down (dns.cache.*, dhcp.*), so summarising the top
		// level as "N sub-keys" meant the tool returned no metrics at all.
		flat := flattenTree(metrics.Metrics)
		if flat == "" {
			return mcp.NewToolResultText("No metrics returned."), nil
		}

		return mcp.NewToolResultText(flat), nil
	}
}

func infoSensorsHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var resp pihole.SensorsInfo
		if err := c.Get(ctx, "/info/sensors", &resp); err != nil {
			return toolError("get sensors", err), nil
		}
		sensors := resp.Sensors

		var b strings.Builder
		for _, group := range sensors.List {
			if len(group.Temps) == 0 {
				continue
			}
			fmt.Fprintf(&b, "**%s:**\n", format.ValueOr(deref(group.Name), group.Path))
			for _, t := range group.Temps {
				fmt.Fprintf(&b, "- %s: %s", format.ValueOr(deref(t.Name), t.Sensor), temperature(t.Value, sensors.Unit))
				var limits []string
				if t.Max != nil {
					limits = append(limits, "high "+temperature(*t.Max, sensors.Unit))
				}
				if t.Crit != nil {
					limits = append(limits, "critical "+temperature(*t.Crit, sensors.Unit))
				}
				if len(limits) > 0 {
					fmt.Fprintf(&b, " (%s)", strings.Join(limits, ", "))
				}
				b.WriteString("\n")
			}
		}
		if b.Len() == 0 {
			if sensors.CPUTemp != nil {
				return mcp.NewToolResultText("**CPU temperature:** " + temperature(*sensors.CPUTemp, sensors.Unit)), nil
			}
			return mcp.NewToolResultText("No temperature sensors are visible to Pi-hole. That is usual in a container or virtual machine."), nil
		}
		if sensors.HotLimit > 0 {
			fmt.Fprintf(&b, "\nPi-hole's web interface warns above %s.", temperature(sensors.HotLimit, sensors.Unit))
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}
