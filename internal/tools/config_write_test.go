package tools

import (
	"io"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
)

// detailedConfig is a GET /config?detailed=true reply covering the settings
// the tests below write, each wrapped with the value and flags FTL publishes.
func detailedConfig() map[string]any {
	setting := func(value any, restarts, endsSessions bool) map[string]any {
		return map[string]any{
			"value": value,
			"flags": map[string]any{"restart_dnsmasq": restarts, "session_reset": endsSessions, "env_var": false},
		}
	}
	return map[string]any{"config": map[string]any{
		"dns": map[string]any{
			"cache":    map[string]any{"size": setting(10000.0, true, false)},
			"blocking": map[string]any{"active": setting(true, false, false)},
		},
		"webserver": map[string]any{"api": map[string]any{"password": setting("********", false, true)}},
	}}
}

// configAfter is the configuration FTL returns from the PATCH.
func configAfter(cacheSize float64) map[string]any {
	return map[string]any{"config": map[string]any{
		"dns":       map[string]any{"cache": map[string]any{"size": cacheSize}, "blocking": map[string]any{"active": true}},
		"webserver": map[string]any{"api": map[string]any{"password": "********"}},
	}}
}

func TestConfigSet_ReportsWhatChangedAndWaitsForTheRestart(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /config":   detailedConfig(),
		"PATCH /config": configAfter(20000),
		"/info/ftl":     map[string]any{"ftl": map[string]any{"uptime": 0}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, configSetHandler, c, map[string]any{"config": `{"dns":{"cache":{"size":20000}}}`})
	for _, want := range []string{"Config updated", "dns.cache.size: 10000 → 20000", "restarted to apply it"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply should contain %q, got:\n%s", want, text)
		}
	}
	if rec.Count("GET", "/info/ftl") == 0 {
		t.Error("dns.cache.size restarts FTL, but the tool did not wait for it")
	}
}

func TestConfigSet_RestartFalseSaysWhenTheChangeTakesEffect(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /config":   detailedConfig(),
		"PATCH /config": configAfter(20000),
	})
	c := newTestClient(t, rec)

	text := callTool(t, configSetHandler, c, map[string]any{"config": `{"dns":{"cache":{"size":20000}}}`, "restart": false})
	if !strings.Contains(text, "take effect when FTL next restarts") {
		t.Errorf("restart=false on a setting that needs a restart should say so, got:\n%s", text)
	}
	rec.AssertCount(t, "GET", "/info/ftl", 0)
}

func TestConfigSet_UnknownKeyIsAnError(t *testing.T) {
	// FTL skips keys it does not know and answers 200, so a typo used to be
	// reported as "Config updated".
	c := newTestClient(t, piholeHandler(map[string]any{
		"GET /config":   detailedConfig(),
		"PATCH /config": configAfter(10000),
	}))

	text := callToolExpectError(t, configSetHandler, c, map[string]any{
		"config": `{"dns":{"cache":{"sise":20000},"blocking":{"active":true}}}`,
	})
	if !strings.Contains(text, "does not recognise dns.cache.sise") {
		t.Errorf("error should name the ignored key, got:\n%s", text)
	}
	if !strings.Contains(text, "Already set: dns.blocking.active") {
		t.Errorf("error should still account for the recognised key, got:\n%s", text)
	}
}

func TestConfigSet_NothingChanged(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /config":   detailedConfig(),
		"PATCH /config": configAfter(10000),
	})
	c := newTestClient(t, rec)

	text := callTool(t, configSetHandler, c, map[string]any{"config": `{"dns":{"cache":{"size":10000}}}`})
	if !strings.Contains(text, "No setting changed") {
		t.Errorf("an unchanged value should say nothing changed, got:\n%s", text)
	}
	// Nothing changed, so FTL does not restart and nothing should wait for it.
	rec.AssertCount(t, "GET", "/info/ftl", 0)
}

func TestConfigSet_PasswordChangeIsReportedAndWarnsAboutSessions(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"GET /config":   detailedConfig(),
		"PATCH /config": configAfter(10000),
	}))

	text := callTool(t, configSetHandler, c, map[string]any{"config": `{"webserver":{"api":{"password":"new-password"}}}`})
	if !strings.Contains(text, "webserver.api.password: updated") {
		t.Errorf("a new password reads back masked but is still a change, got:\n%s", text)
	}
	if !strings.Contains(text, "update PIHOLE_PASSWORD") {
		t.Errorf("a change that ends every session should warn about PIHOLE_PASSWORD, got:\n%s", text)
	}
	if strings.Contains(text, "new-password") {
		t.Errorf("the reply must not echo the password, got:\n%s", text)
	}
}

func TestConfigAddValue_WaitsWhenTheElementRestartsFTL(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /config/dns/upstreams": map[string]any{"config": map[string]any{"dns": map[string]any{
			"upstreams": map[string]any{"value": []any{"8.8.8.8"}, "flags": map[string]any{"restart_dnsmasq": true}},
		}}},
		"PUT /config/dns/upstreams/1.1.1.1": map[string]any{},
		"/info/ftl":                         map[string]any{"ftl": map[string]any{"uptime": 0}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, configAddValueHandler, c, map[string]any{"element": "dns.upstreams", "value": "1.1.1.1"})
	if !strings.Contains(text, "restarted to apply it") {
		t.Errorf("adding an upstream restarts FTL; the reply should confirm it came back, got:\n%s", text)
	}
}

func TestLocalCNAMEAdd_WaitsForFTLToRestart(t *testing.T) {
	f := piholefake.New()
	t.Cleanup(f.Close)
	c := pihole.New(f.URL(), "x")

	text := callTool(t, localCNAMEAddHandler, c, map[string]any{"alias": "files.home", "target": "nas.home"})
	if !strings.Contains(text, "restarted to apply it") || f.Restarts() != 1 {
		t.Errorf("a CNAME write restarts FTL once and waits for it: restarts=%d, reply:\n%s", f.Restarts(), text)
	}
}

func TestLocalDNSAdd_DoesNotWaitForARestartThatNeverComes(t *testing.T) {
	// dns.hosts carries no restart flag, so waiting would stall every host
	// record write for the full timeout.
	f := piholefake.New()
	t.Cleanup(f.Close)
	c := pihole.New(f.URL(), "x")

	text := callTool(t, localDNSAddHandler, c, map[string]any{"ip": "10.0.0.5", "hostname": "nas.home"})
	if strings.Contains(text, "restart") || f.Restarts() != 0 {
		t.Errorf("a host record write should not restart or wait: restarts=%d, reply:\n%s", f.Restarts(), text)
	}
}

func TestToolError_UncertainWriteSaysToCheckFirst(t *testing.T) {
	err := &pihole.UncertainError{Err: io.ErrUnexpectedEOF}
	res := toolError("add domain", err)
	text := resultText(res)
	if !strings.Contains(text, "may already have made this change") || !strings.Contains(text, "before trying again") {
		t.Errorf("an unanswered write should warn against a blind retry, got: %s", text)
	}
}

func TestConfigSet_DottedKeysAreSentNested(t *testing.T) {
	// FTL reads only nested objects. It skipped a dotted key as unknown and
	// still answered 200, and the tool reported the change as made.
	rec := piholeHandler(map[string]any{
		"GET /config":   detailedConfig(),
		"PATCH /config": configAfter(20000),
		"/info/ftl":     map[string]any{"ftl": map[string]any{"uptime": 0}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, configSetHandler, c, map[string]any{"config": `{"dns.cache.size":20000}`})
	if !strings.Contains(text, "dns.cache.size: 10000 → 20000") {
		t.Errorf("reply should report the change, got:\n%s", text)
	}
	patch := rec.Only(t, "PATCH", "/config")
	patch.AssertField(t, "config.dns.cache.size", 20000.0)
	if _, ok := patch.JSON(t)["config"].(map[string]any)["dns.cache.size"]; ok {
		t.Error("the dotted key was sent as it was given, which FTL ignores")
	}
}

func TestConfigSet_ASettingNamedTwiceIsRefused(t *testing.T) {
	for name, payload := range map[string]string{
		"dotted and nested":   `{"dns.cache.size":1,"dns":{"cache":{"size":2}}}`,
		"differing in case":   `{"dns":{"cache":{"size":1}},"DNS":{"cache":{"size":2}}}`,
		"value and a section": `{"dns.cache":5,"dns.cache.size":1}`,
		"empty segment":       `{"dns..size":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := piholeHandler(map[string]any{"GET /config": detailedConfig()})
			c := newTestClient(t, rec)
			callToolExpectError(t, configSetHandler, c, map[string]any{"config": payload})
			rec.AssertNone(t, "PATCH", "")
		})
	}
}

func TestConfigSet_KeysMatchIgnoringCaseAsFTLDoes(t *testing.T) {
	// FTL finds config keys ignoring case, so it applied this write while the
	// tool reported the key as unknown and skipped the restart wait.
	rec := piholeHandler(map[string]any{
		"GET /config":   detailedConfig(),
		"PATCH /config": configAfter(20000),
		"/info/ftl":     map[string]any{"ftl": map[string]any{"uptime": 0}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, configSetHandler, c, map[string]any{"config": `{"DNS":{"Cache":{"Size":20000}}}`})
	if !strings.Contains(text, "dns.cache.size: 10000 → 20000") {
		t.Errorf("reply should report the change under FTL's spelling, got:\n%s", text)
	}
	if rec.Count("GET", "/info/ftl") == 0 {
		t.Error("dns.cache.size restarts FTL, but the tool did not wait for it")
	}
}

func TestExpandConfigKeys_MergesSectionsSpelledDifferently(t *testing.T) {
	got, err := expandConfigKeys(map[string]any{
		"dns.cache.size": 1.0,
		"DNS":            map[string]any{"blocking": map[string]any{"active": true}},
		"webserver":      map[string]any{"api.password": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two sections, got %v", got)
	}
	leaves := map[string]any{}
	configLeaves("", got, leaves)
	if len(leaves) != 3 {
		t.Errorf("leaves = %v, want three settings", leaves)
	}
}
