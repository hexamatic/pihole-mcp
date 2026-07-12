package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestConfigGet_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config": map[string]any{
			"config": map[string]any{
				"dns":       map[string]any{"blocking": map[string]any{"active": true}},
				"webserver": map[string]any{"port": 80},
			},
		},
	}))

	text := callTool(t, configGetHandler, c, map[string]any{"detail": "minimal"})
	if !strings.Contains(text, "Config sections:") {
		t.Errorf("expected section list, got: %s", text)
	}
	if !strings.Contains(text, "dns") {
		t.Errorf("expected 'dns' section name, got: %s", text)
	}
	if !strings.Contains(text, "webserver") {
		t.Errorf("expected 'webserver' section name, got: %s", text)
	}
}

func TestConfigGet_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config": map[string]any{
			"config": map[string]any{
				"dns": map[string]any{"blocking": map[string]any{"active": true}},
			},
		},
	}))

	text := callTool(t, configGetHandler, c, nil)
	if !strings.Contains(text, "**dns:**") {
		t.Errorf("expected section summary, got: %s", text)
	}
	if !strings.Contains(text, "1 settings") {
		t.Errorf("expected settings count, got: %s", text)
	}
}

func TestConfigGet_Full(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config": map[string]any{
			"config": map[string]any{
				"dns": map[string]any{"blocking": map[string]any{"active": true}},
			},
		},
	}))

	text := callTool(t, configGetHandler, c, map[string]any{"detail": "full"})
	if !strings.Contains(text, "```json") {
		t.Errorf("expected JSON code block, got: %s", text)
	}
	if !strings.Contains(text, "blocking") {
		t.Errorf("expected config content, got: %s", text)
	}
}

func TestConfigGet_WithSection(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config":     map[string]any{"config": map[string]any{"dns": map[string]any{}, "webserver": map[string]any{}}},
		"/config/dns": map[string]any{"config": map[string]any{"blocking": map[string]any{"active": true}}},
	}))

	text := callTool(t, configGetHandler, c, map[string]any{"section": "dns"})
	if !strings.Contains(text, "blocking") {
		t.Errorf("expected dns section content, got: %s", text)
	}
}

func TestConfigSet_Success(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config": map[string]any{
			"config": map[string]any{"dns": map[string]any{"blocking": map[string]any{"active": false}}},
		},
	}))

	text := callTool(t, configSetHandler, c, map[string]any{
		"config": `{"dns":{"blocking":{"active":false}}}`,
	})
	if !strings.Contains(text, "Config updated") {
		t.Errorf("expected 'Config updated' message, got: %s", text)
	}
}

func TestConfigSet_MissingParam(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config": map[string]any{"config": map[string]any{}},
	}))

	text := callToolExpectError(t, configSetHandler, c, nil)
	if !strings.Contains(text, "config") {
		t.Errorf("expected error mentioning 'config' param, got: %s", text)
	}
}

func TestConfigSet_Error(t *testing.T) {
	c := newTestClient(t, piholeErrorServer(400, "bad_request", "Invalid config", "Check JSON"))

	text := callToolExpectError(t, configSetHandler, c, map[string]any{
		"config": `{"invalid": true}`,
	})
	if text == "" {
		t.Error("expected error text, got empty string")
	}
}

func TestConfigGetValue_Success(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config/dns/upstreams": map[string]any{
			"config": map[string]any{
				"upstreams": []any{"1.1.1.1#53", "8.8.8.8#53"},
			},
		},
	}))

	text := callTool(t, configGetValueHandler, c, map[string]any{"element": "dns.upstreams"})
	if !strings.Contains(text, "dns/upstreams") {
		t.Errorf("expected element path in output, got: %s", text)
	}
	if !strings.Contains(text, "1.1.1.1#53") {
		t.Errorf("expected upstream value, got: %s", text)
	}
}

func TestConfigAddValue_Success(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config/dns/upstreams/1.1.1.1": map[string]any{
			"config": map[string]any{
				"upstreams": []any{"1.1.1.1"},
			},
		},
	}))

	text := callTool(t, configAddValueHandler, c, map[string]any{
		"element": "dns.upstreams",
		"value":   "1.1.1.1",
	})
	if !strings.Contains(text, "Added") {
		t.Errorf("expected 'Added' message, got: %s", text)
	}
	if !strings.Contains(text, "1.1.1.1") {
		t.Errorf("expected value in output, got: %s", text)
	}
}

func TestConfigRemoveValue_Success(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config/dns/upstreams/1.1.1.1": map[string]any{},
	}))

	text := callTool(t, configRemoveValueHandler, c, map[string]any{
		"element": "dns.upstreams",
		"value":   "1.1.1.1",
	})
	if !strings.Contains(text, "Removed") {
		t.Errorf("expected 'Removed' message, got: %s", text)
	}
	if !strings.Contains(text, "1.1.1.1") {
		t.Errorf("expected value in output, got: %s", text)
	}
}

func TestConfigProperties_Fixture(t *testing.T) {
	fixture := loadFixture(t, "config_properties")

	c := newTestClient(t, piholeHandler(map[string]any{
		"/config/_properties": fixture,
	}))

	// The number of read-only keys depends on how many settings the dev Pi-hole
	// pins through FTLCONF_* environment variables, so derive it from the
	// fixture rather than freezing a number that a compose change would break.
	cfg, ok := fixture.(map[string]any)["config"].(map[string]any)
	if !ok {
		t.Fatal("config_properties fixture has no config object")
	}
	readOnly, ok := cfg["read_only"].([]any)
	if !ok || len(readOnly) == 0 {
		t.Fatal("config_properties fixture has no read_only entries — re-run `just refresh-fixtures`")
	}

	text := callTool(t, configPropertiesHandler, c, nil)
	if want := fmt.Sprintf("%d read-only", len(readOnly)); !strings.Contains(text, want) {
		t.Errorf("expected %q in output, got: %s", want, text)
	}
	if !strings.Contains(text, "misc.readOnly") {
		t.Errorf("expected misc.readOnly key in output, got: %s", text)
	}
	if !strings.Contains(text, "env_var") {
		t.Errorf("expected env_var reason in output, got: %s", text)
	}
}

func TestConfigProperties_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config/_properties": loadFixture(t, "config_properties"),
	}))

	text := callTool(t, configPropertiesHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "Key,Reason,Description") {
		t.Errorf("expected CSV header, got: %s", text)
	}
	if !strings.Contains(text, "misc.readOnly,read_only,") {
		t.Errorf("expected CSV row for misc.readOnly, got: %s", text)
	}
}

func TestConfigProperties_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config/_properties": map[string]any{
			"config": map[string]any{"read_only": []any{}},
			"took":   0.0,
		},
	}))

	text := callTool(t, configPropertiesHandler, c, nil)
	if !strings.Contains(text, "No read-only config keys reported") {
		t.Errorf("expected empty-state message, got: %s", text)
	}
}
