package tools

import (
	"strings"
	"testing"
)

func TestInfoSystem_EmptyUnits(t *testing.T) {
	// Simulates Docker environment where Pi-hole returns empty units.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/system": map[string]any{
			"system": map[string]any{
				"uptime": 3600,
				"load":   []any{0.5, 0.3, 0.1},
				"cpu":    map[string]any{"nprocs": 4, "perc": 15.5},
				"memory": map[string]any{
					"ram": map[string]any{
						"total": 8025148.0, "used": 543304.0, "free": 7481844.0,
						"perc": 6.8, "unit": "", // Empty unit — Docker quirk
					},
				},
				"disk": map[string]any{
					"total": 0.0, "used": 0.0, "free": 0.0,
					"perc": 0.0, "unit": "", // Empty unit
				},
				"dns": map[string]any{"running": false},
			},
		},
		"/info/host": map[string]any{
			"host": map[string]any{
				"name": "pihole-dev", "os": "Linux", "arch": "aarch64",
				"kernel": "6.8.0", "domain": "",
			},
		},
		"/info/sensors": map[string]any{
			"sensors": map[string]any{"list": []any{}},
		},
	}))

	text := callTool(t, infoSystemHandler, c, nil)

	// Should show auto-converted bytes, not "543304.0/8025148.0 "
	if strings.Contains(text, "543304") {
		t.Errorf("raw bytes should be formatted, got: %s", text)
	}
	if !strings.Contains(text, "KB") && !strings.Contains(text, "MB") && !strings.Contains(text, "GB") {
		t.Errorf("expected auto-formatted size unit, got: %s", text)
	}

	// DNS not running should have Docker context
	if strings.Contains(text, "DNS:** not running\n") {
		t.Errorf("DNS not running should have Docker context, got: %s", text)
	}
	if !strings.Contains(text, "expected in Docker") {
		t.Errorf("should mention Docker context for DNS, got: %s", text)
	}
}

func TestInfoSystem_RealFixture(t *testing.T) {
	// Real captured response — protects against shape drift.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/system":  loadFixture(t, "info_system"),
		"/info/host":    map[string]any{"host": map[string]any{"name": "pihole-dev"}},
		"/info/sensors": loadFixture(t, "info_sensors"),
	}))

	text := callTool(t, infoSystemHandler, c, nil)
	// Just confirm the handler doesn't crash and produces non-empty output.
	if text == "" {
		t.Fatal("expected non-empty system info from real fixture")
	}
}

func TestInfoSystem_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/system": map[string]any{
			"system": map[string]any{
				"uptime": 3600,
				"load":   []any{0.5, 0.3, 0.1},
				"cpu":    map[string]any{"nprocs": 4, "perc": 15.5},
				"memory": map[string]any{
					"ram": map[string]any{
						"total": 8000000.0, "used": 500000.0, "free": 7500000.0,
						"perc": 6.3, "unit": "kB",
					},
				},
				"disk": map[string]any{"total": 50.0, "used": 20.0, "free": 30.0, "perc": 40.0, "unit": "GB"},
				"dns":  map[string]any{"running": true},
			},
		},
	}))

	text := callTool(t, infoSystemHandler, c, map[string]any{"detail": "minimal"})
	if strings.Count(text, "\n") > 1 {
		t.Errorf("minimal should be single-line, got: %s", text)
	}
	if !strings.Contains(text, "Load:") {
		t.Errorf("minimal should contain load, got: %s", text)
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
	if !strings.Contains(text, "dns_cache_size") {
		t.Errorf("expected 'dns_cache_size' key, got: %s", text)
	}
	if !strings.Contains(text, "reply_NODATA") {
		t.Errorf("expected 'reply_NODATA' key, got: %s", text)
	}
	if !strings.Contains(text, "dhcp") {
		t.Errorf("expected 'dhcp' key, got: %s", text)
	}
	if !strings.Contains(text, "sub-keys") {
		t.Errorf("expected 'sub-keys' for nested map, got: %s", text)
	}
}

func TestInfoSensors_RealFixture(t *testing.T) {
	// Real fixture has an empty sensors list (Docker dev container has no sensors).
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/sensors": loadFixture(t, "info_sensors"),
	}))

	text := callTool(t, infoSensorsHandler, c, nil)
	if !strings.Contains(text, "No sensor data available") {
		t.Errorf("expected empty sensors message from Docker fixture, got: %s", text)
	}
}

func TestInfoSensors_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/sensors": map[string]any{
			"sensors": map[string]any{
				"list": []any{
					map[string]any{"name": "cpu_thermal", "value": 52.3, "unit": "°C", "path": "/sys/class/thermal/thermal_zone0/temp"},
				},
			},
		},
	}))

	text := callTool(t, infoSensorsHandler, c, nil)
	if !strings.Contains(text, "cpu_thermal") {
		t.Errorf("expected sensor name 'cpu_thermal', got: %s", text)
	}
	if !strings.Contains(text, "52.3") {
		t.Errorf("expected sensor value '52.3', got: %s", text)
	}
}

func TestInfoSensors_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/sensors": map[string]any{
			"sensors": map[string]any{"list": []any{}},
		},
	}))

	text := callTool(t, infoSensorsHandler, c, nil)
	if !strings.Contains(text, "No sensor data available") {
		t.Errorf("expected empty sensors message, got: %s", text)
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
	c := newTestClient(t, piholeHandler(map[string]any{
		"/info/messages/1": map[string]any{},
	}))

	text := callTool(t, infoDismissMessageHandler, c, map[string]any{"id": float64(1)})
	if !strings.Contains(text, "Dismissed diagnostic message 1") {
		t.Errorf("expected a confirmation, got: %s", text)
	}
}

func TestInfoDismissMessage_RequiresID(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{}))

	text := callToolExpectError(t, infoDismissMessageHandler, c, nil)
	if !strings.Contains(text, "pihole_info_messages") {
		t.Errorf("the missing-id error should point at pihole_info_messages, got: %s", text)
	}
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
