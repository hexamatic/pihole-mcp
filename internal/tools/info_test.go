package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/format"
)

// systemRoutes serves one version's captures of the three endpoints
// pihole_info_system reads.
func systemRoutes(t *testing.T, version string) map[string]any {
	return map[string]any{
		"/info/system":  loadVersionFixture(t, version, "info_system"),
		"/info/host":    loadVersionFixture(t, version, "info_host"),
		"/info/sensors": loadVersionFixture(t, version, "info_sensors"),
	}
}

func TestInfoSystem_ReportsWhatFTLSends(t *testing.T) {
	// The decoder read "perc", "load", "disk" and "dns", none of which FTL
	// sends, and the host fields from the wrong level, so every Pi-hole
	// showed 0% CPU, zero load, 0% disk, a stopped resolver and no host, with
	// memory printed as bytes when FTL counts kilobytes.
	eachFixture(t, "info_system", func(t *testing.T, version string, sys any) {
		routes := systemRoutes(t, version)
		c := newTestClient(t, piholeHandler(routes))
		text := callTool(t, infoSystemHandler, c, nil)

		host := routes["/info/host"]
		ramUsed := fixtureValue(t, sys, "system.memory.ram.used").(float64)
		load := fixtureValue(t, sys, "system.cpu.load.raw").([]any)
		for _, want := range []string{
			"**Host:** " + fixtureString(t, host, "host.uname.nodename") + " (" +
				fixtureString(t, host, "host.uname.sysname") + " " + fixtureString(t, host, "host.uname.machine") + ")",
			fmt.Sprintf("**Load:** %.2f, %.2f, %.2f", load[0], load[1], load[2]),
			fmt.Sprintf("**CPU:** %d cores, %.1f%% used", fixtureInt(t, sys, "system.cpu.nprocs"),
				fixtureValue(t, sys, "system.cpu.%cpu")),
			"**Memory:** " + format.Bytes(ramUsed*1024) + " of ",
			fmt.Sprintf("**FTL:** %.1f%% CPU", fixtureValue(t, sys, "system.ftl.%cpu")),
		} {
			if !strings.Contains(text, want) {
				t.Errorf("expected %q in system info, got:\n%s", want, text)
			}
		}
		// FTL reports neither, so neither may be invented.
		for _, absent := range []string{"Disk", "DNS:"} {
			if strings.Contains(text, absent) {
				t.Errorf("system info should not report %q, which FTL does not send, got:\n%s", absent, text)
			}
		}
	})
}

func TestInfoSystem_FullAddsTheKernel(t *testing.T) {
	eachFixture(t, "info_host", func(t *testing.T, version string, host any) {
		c := newTestClient(t, piholeHandler(systemRoutes(t, version)))
		text := callTool(t, infoSystemHandler, c, map[string]any{"detail": "full"})
		want := "**Kernel:** " + fixtureString(t, host, "host.uname.sysname") + " " + fixtureString(t, host, "host.uname.release")
		if !strings.Contains(text, want) {
			t.Errorf("expected %q at full detail, got:\n%s", want, text)
		}
		// The containers report no NIS domain, which uname spells "(none)".
		if strings.Contains(text, "(none)") {
			t.Errorf("an unset domain should be left out, got:\n%s", text)
		}
	})
}

func TestInfoSystem_Minimal(t *testing.T) {
	eachFixture(t, "info_system", func(t *testing.T, version string, sys any) {
		c := newTestClient(t, piholeHandler(systemRoutes(t, version)))
		text := callTool(t, infoSystemHandler, c, map[string]any{"detail": "minimal"})
		want := fmt.Sprintf("Load: %.2f | CPU: %.0f%%", fixtureValue(t, sys, "system.cpu.load.raw").([]any)[0],
			fixtureValue(t, sys, "system.cpu.%cpu"))
		if !strings.HasPrefix(text, want) || strings.Contains(text, "\n") {
			t.Errorf("expected a single line starting %q, got: %s", want, text)
		}
	})
}

func TestInfoSystem_CPUTemperature(t *testing.T) {
	routes := systemRoutes(t, "v6.8")
	routes["/info/sensors"] = map[string]any{"sensors": map[string]any{
		"list": []any{}, "cpu_temp": 48.25, "hot_limit": 60.0, "unit": "F",
	}}
	c := newTestClient(t, piholeHandler(routes))
	text := callTool(t, infoSystemHandler, c, nil)
	if !strings.Contains(text, "**CPU temperature:** 48.2°F (warns above 60.0°F)") {
		t.Errorf("expected the CPU temperature in Pi-hole's unit, got:\n%s", text)
	}
}

// TestInfoFTL_RealFixture decodes the captured response from a live Pi-hole
// instance. The real fixture catches type-shape drift that the original
// hand-written mock missed (clients-as-int, database.queries field) and
// triggered the v0.2.0 E2E crash documented in TODO #6.
func TestInfoFTL_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/ftl": loadFixture(t, "info_ftl"),
	}))

	text := callTool(t, infoFTLHandler, c, nil)
	if !strings.Contains(text, "PID:") {
		t.Errorf("expected PID label in output, got: %s", text)
	}
	if !strings.Contains(text, "Privacy level:") {
		t.Errorf("expected privacy level label, got: %s", text)
	}
	if !strings.Contains(text, "Active clients:") {
		t.Errorf("expected active clients label, got: %s", text)
	}
	if !strings.Contains(text, "Gravity domains:") {
		t.Errorf("expected gravity domains label, got: %s", text)
	}
}

func TestInfoFTL_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/ftl": map[string]any{
			"ftl": map[string]any{
				"pid":               1234,
				"uptime":            12345.6,
				"privacy_level":     0,
				"query_frequency":   2.5,
				"clients":           map[string]any{"total": 50, "active": 42},
				"%mem":              0.13,
				"%cpu":              0.14,
				"allow_destructive": true,
				"database": map[string]any{
					"gravity": 92277,
					"groups":  1,
					"lists":   3,
					"clients": 0,
				},
				"dnsmasq": map[string]any{"dns_cache_inserted": 0},
			},
		},
	}))

	text := callTool(t, infoFTLHandler, c, nil)
	if !strings.Contains(text, "1,234") {
		t.Errorf("expected formatted PID '1,234', got: %s", text)
	}
	if !strings.Contains(text, "Privacy level") {
		t.Errorf("expected 'Privacy level' label, got: %s", text)
	}
	if !strings.Contains(text, "92,277") {
		t.Errorf("expected formatted gravity count '92,277', got: %s", text)
	}
	if !strings.Contains(text, "42 of 50") {
		t.Errorf("expected active/total client format '42 of 50', got: %s", text)
	}
}

func TestInfoFTL_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/ftl": map[string]any{
			"ftl": map[string]any{
				"pid":           1234,
				"privacy_level": 2,
				"clients":       map[string]any{"total": 50, "active": 42},
				"database":      map[string]any{"gravity": 92277},
			},
		},
	}))

	text := callTool(t, infoFTLHandler, c, map[string]any{"detail": "minimal"})
	if strings.Count(text, "\n") > 1 {
		t.Errorf("minimal should be single-line, got: %s", text)
	}
	if !strings.Contains(text, "Privacy: 2") {
		t.Errorf("minimal should show privacy level, got: %s", text)
	}
}

func TestInfoMetrics_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/metrics": loadFixture(t, "info_metrics"),
	}))

	text := callTool(t, infoMetricsHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty metrics output from real fixture")
	}
	// dns and dhcp are top-level keys in the captured fixture; verify both render.
	if !strings.Contains(text, "dns") {
		t.Errorf("expected 'dns' top-level metric, got: %s", text)
	}
	if !strings.Contains(text, "dhcp") {
		t.Errorf("expected 'dhcp' top-level metric, got: %s", text)
	}
}

func TestInfoMetrics_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/metrics": map[string]any{
			"metrics": map[string]any{
				"dns_cache_size": 10000,
				"reply_NODATA":   512,
				"dhcp": map[string]any{
					"leases_allocated": 15,
					"leases_pruned":    3,
				},
			},
		},
	}))

	text := callTool(t, infoMetricsHandler, c, nil)
	want := "dhcp.leases_allocated: 15\n" +
		"dhcp.leases_pruned: 3\n" +
		"dns_cache_size: 10000\n" +
		"reply_NODATA: 512\n"
	if text != want {
		t.Errorf("got:\n%s\nwant:\n%s", text, want)
	}
	// Every metric FTL reports lives under a nested key, so the old
	// "dhcp: 2 sub-keys" rendering meant the tool never returned a metric.
	if strings.Contains(text, "sub-keys") {
		t.Errorf("expected metric values rather than sub-key counts, got: %s", text)
	}
}

func TestInfoMetrics_MinimalNamesGroups(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/metrics": map[string]any{
			"metrics": map[string]any{
				"dns":  map[string]any{"cache": map[string]any{"size": 10000}},
				"dhcp": map[string]any{"leases_allocated": 15},
			},
		},
	}))

	text := callTool(t, infoMetricsHandler, c, map[string]any{"detail": "minimal"})
	if text != "Metric groups: dhcp, dns" {
		t.Errorf("got %q, want the group names in sorted order", text)
	}
}

// The real fixture is the shape that matters: a cache-hit number has to reach
// the caller at the default detail level.
func TestInfoMetrics_FixtureExposesCacheNumbers(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/metrics": loadFixture(t, "info_metrics"),
	}))

	text := callTool(t, infoMetricsHandler, c, nil)
	if !strings.Contains(text, "dns.cache.size: ") {
		t.Errorf("expected a dns.cache.size value, got: %s", text)
	}
	if strings.Contains(text, "sub-keys") {
		t.Errorf("expected values rather than sub-key counts, got: %s", text)
	}
}

func TestInfoSensors_RealFixture(t *testing.T) {
	// The dev containers expose no hardware monitors, so both captures carry
	// an empty list and no CPU temperature.
	eachFixture(t, "info_sensors", func(t *testing.T, _ string, fixture any) {
		c := newTestClient(t, piholeHandler(map[string]any{"/info/sensors": fixture}))
		text := callTool(t, infoSensorsHandler, c, nil)
		if !strings.Contains(text, "No temperature sensors are visible") {
			t.Errorf("expected the no-sensors message, got: %s", text)
		}
	})
}

func TestInfoSensors_ReadsEachMonitorsReadings(t *testing.T) {
	// FTL groups readings by hardware monitor, each with a temps list, and
	// gives the unit once for all of them (src/api/info.c, v6.7 and v6.8).
	// The decoder read the monitors as readings, so a Raspberry Pi showed
	// one nameless 0.0 per monitor.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/sensors": map[string]any{"sensors": map[string]any{
			"list": []any{
				map[string]any{
					"name": "cpu_thermal", "path": "hwmon0", "source": "/sys/devices/virtual/thermal/thermal_zone0",
					"temps": []any{
						map[string]any{"name": nil, "value": 52.3, "max": nil, "crit": 110.0, "sensor": "temp1"},
					},
				},
				map[string]any{
					"name": nil, "path": "hwmon1", "source": "",
					"temps": []any{
						map[string]any{"name": "Package id 0", "value": 61.0, "max": 80.0, "crit": 100.0, "sensor": "temp1"},
					},
				},
			},
			"cpu_temp": 52.3, "hot_limit": 60.0, "unit": "C",
		}},
	}))

	text := callTool(t, infoSensorsHandler, c, nil)
	for _, want := range []string{
		"**cpu_thermal:**\n- temp1: 52.3°C (critical 110.0°C)",
		"**hwmon1:**\n- Package id 0: 61.0°C (high 80.0°C, critical 100.0°C)",
		"warns above 60.0°C",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q, got:\n%s", want, text)
		}
	}
}

// TestInfoDatabase_RealFixture catches the v0.2.0 shape regression: the real
// /api/info/database response is FLAT (queries, size, sqlite_version at the
// top level), not nested under a `database` key as the original hand-written
// mock assumed.
func TestInfoMessages_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/messages": loadFixture(t, "info_messages"),
	}))

	text := callTool(t, infoMessagesHandler, c, nil)

	// The text of a diagnostic message lives in "plain". Decoding the wrong key
	// left this tool printing a type and a timestamp and nothing else, which is
	// what this fixture exists to prevent recurring.
	if !strings.Contains(text, "was inaccessible during last gravity run") {
		t.Errorf("message text is missing from the output, got: %s", text)
	}
	if !strings.Contains(text, "[LIST]") {
		t.Errorf("expected the message type, got: %s", text)
	}
	// Without the ID there is no way to call pihole_info_dismiss_message.
	if !strings.Contains(text, "id=1") {
		t.Errorf("expected the message ID so it can be dismissed, got: %s", text)
	}
}

func TestInfoMessages_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/messages": map[string]any{"messages": []any{}},
	}))

	text := callTool(t, infoMessagesHandler, c, nil)
	if !strings.Contains(text, "No diagnostic messages") {
		t.Errorf("expected the empty-state message, got: %s", text)
	}
}

func TestInfoDismissMessage_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/info/messages/1": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, infoDismissMessageHandler, c, map[string]any{"id": float64(1)})
	if !strings.Contains(text, "Dismissed diagnostic message 1") {
		t.Errorf("expected a confirmation, got: %s", text)
	}

	// The confirmation is formatted from the tool's own id argument, so it
	// reads the same after a request that dismissed nothing. The id is
	// interpolated into the path with fmt.Sprintf, which is why the raw path
	// is worth pinning as well as the verb.
	req := rec.Only(t, "DELETE", "/info/messages/1")
	req.AssertRawPath(t, "/info/messages/1")
	req.AssertNoBody(t)
	req.AssertNoQueryString(t)
}

func TestInfoDismissMessage_RequiresID(t *testing.T) {
	rec := piholeHandler(map[string]any{})
	c := newTestClient(t, rec)

	text := callToolExpectError(t, infoDismissMessageHandler, c, nil)
	if !strings.Contains(text, "pihole_info_messages") {
		t.Errorf("the missing-id error should point at pihole_info_messages, got: %s", text)
	}

	// A missing id must not become DELETE /info/messages/0.
	rec.AssertNone(t, "", "")
}

func TestInfoDatabase_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/database": loadFixture(t, "info_database"),
	}))

	text := callTool(t, infoDatabaseHandler, c, nil)
	if !strings.Contains(text, "Size:") {
		t.Errorf("expected 'Size:' label, got: %s", text)
	}
	if !strings.Contains(text, "SQLite:") {
		t.Errorf("expected 'SQLite:' label, got: %s", text)
	}
	// Assert against whatever the fixture actually holds rather than a literal:
	// SQLite ships inside FTL, so pinning the version here means every Pi-hole
	// bump breaks this test for no good reason. What we care about is that the
	// flat structure decoded and the value reached the output.
	fx, ok := loadFixture(t, "info_database").(map[string]any)
	if !ok {
		t.Fatal("info_database fixture is not a JSON object")
	}
	wantSQLite, ok := fx["sqlite_version"].(string)
	if !ok || wantSQLite == "" {
		t.Fatal("info_database fixture has no sqlite_version")
	}
	if !strings.Contains(text, wantSQLite) {
		t.Errorf("expected sqlite version %q from the fixture, got: %s", wantSQLite, text)
	}
	if strings.Contains(text, "Size: 0  | Queries: 0 | SQLite: N/A") {
		t.Errorf("flat-structure decode failed — handler still expects nested database key, got: %s", text)
	}
}

func TestInfoDatabase_EmptyFields(t *testing.T) {
	// Edge case: real Pi-hole on a fresh install can produce an empty
	// sqlite_version. Our handler should render N/A in that case.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/database": map[string]any{
			"size": 0.0, "queries": 0, "sqlite_version": "",
		},
	}))

	text := callTool(t, infoDatabaseHandler, c, nil)
	if !strings.Contains(text, "N/A") {
		t.Errorf("expected N/A for empty SQLite version, got: %s", text)
	}
}

func TestInfoMetrics_NormalEmptyMetrics(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/metrics": map[string]any{"metrics": map[string]any{}},
	}))

	if text := callTool(t, infoMetricsHandler, c, nil); text != "No metrics returned." {
		t.Errorf("got %q, want a named empty result", text)
	}
}

// FTL reports 0 for a timestamp it does not have yet. Rendering that through
// the timestamp formatter would put 1970 in front of the caller.
func TestInfoDatabase_ZeroTimestampsNotRendered(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/database": map[string]any{
			"size": 90112, "queries": 27, "sqlite_version": "3.53.1",
			"earliest_timestamp": 1783733685.9495127, "earliest_timestamp_disk": 0,
		},
	}))

	text := callTool(t, infoDatabaseHandler, c, nil)
	if !strings.Contains(text, "**Earliest query:**") {
		t.Errorf("expected the earliest query date, got: %s", text)
	}
	if strings.Contains(text, "Earliest on disk") {
		t.Errorf("reported an on-disk date FTL does not have yet: %s", text)
	}
	if strings.Contains(text, "1970") {
		t.Errorf("rendered a zero timestamp as a date: %s", text)
	}
}
