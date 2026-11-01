package tools

import (
	"strings"
	"testing"
)

// riskyDetailed is a detailed config read holding one risky setting and one
// ordinary one, each wrapped with its value and flags as FTL publishes them.
func riskyDetailed() map[string]any {
	setting := func(v any) map[string]any {
		return map[string]any{"value": v, "flags": map[string]any{"restart_dnsmasq": false, "session_reset": false}}
	}
	return map[string]any{"config": map[string]any{
		"webserver": map[string]any{
			"port":  setting("80o,443os"),
			"paths": map[string]any{"webroot": setting("/var/www/html")},
		},
		"misc": map[string]any{"dnsmasq_lines": setting([]any{})},
	}}
}

func TestRiskySettingFor(t *testing.T) {
	for path, want := range map[string]bool{
		"misc.dnsmasq_lines":       true,
		"FILES.Gravity":            true,
		"webserver.paths":          true,
		"webserver.paths.webroot":  true,
		"webserver.advancedOpts":   true,
		"webserver.port":           false,
		"dns.upstreams":            false,
		"misc.dnsmasq_lines_extra": false,
	} {
		if _, got := riskySettingFor(path); got != want {
			t.Errorf("riskySettingFor(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestConfigSet_RefusesRiskySettings(t *testing.T) {
	for name, payload := range map[string]string{
		"dnsmasq lines": `{"misc":{"dnsmasq_lines":["dhcp-script=/bin/sh"]}}`,
		"dotted":        `{"files.gravity":"/tmp/elsewhere.db"}`,
		"webroot moved": `{"webserver":{"paths":{"webroot":"/"}}}`,
		"line break":    `{"webserver":{"port":"80\nserver=1.2.3.4"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := piholeHandler(map[string]any{"GET /config": riskyDetailed()})
			text := callToolExpectError(t, configSetHandler, newTestClient(t, rec), map[string]any{"config": payload})
			if !strings.Contains(text, "Refusing") {
				t.Errorf("expected a refusal, got: %s", text)
			}
			rec.AssertNone(t, "PATCH", "")
		})
	}
}

func TestConfigSet_WritingASectionBackUnchangedIsAllowed(t *testing.T) {
	// Reading a section and writing it back is how a caller changes one
	// setting in it; the risky settings it carries are unchanged.
	rec := piholeHandler(map[string]any{
		"GET /config":   riskyDetailed(),
		"PATCH /config": map[string]any{"config": map[string]any{"webserver": map[string]any{"port": "8080", "paths": map[string]any{"webroot": "/var/www/html"}}}},
	})
	callTool(t, configSetHandler, newTestClient(t, rec), map[string]any{
		"config": `{"webserver":{"port":"8080","paths":{"webroot":"/var/www/html"}}}`,
	})
	rec.AssertCount(t, "PATCH", "/config", 1)
}

func TestConfigAddValue_RefusesRiskySettingsAndLineBreaks(t *testing.T) {
	rec := piholeHandler(nil)
	c := newTestClient(t, rec)
	callToolExpectError(t, configAddValueHandler, c, map[string]any{"element": "misc.dnsmasq_lines", "value": "dhcp-script=/bin/sh"})
	callToolExpectError(t, configAddValueHandler, c, map[string]any{"element": "dns.upstreams", "value": "8.8.8.8\nserver=1.2.3.4"})
	callToolExpectError(t, configRemoveValueHandler, c, map[string]any{"element": "files.gravity", "value": "x"})
	rec.AssertNone(t, "", "")
}
