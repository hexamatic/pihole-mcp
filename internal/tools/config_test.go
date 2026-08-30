package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/server"
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
	if !strings.Contains(text, "dns.blocking.active: true") {
		t.Errorf("expected the flattened setting and its value, got: %s", text)
	}
	// The old normal detail rendered "**dns:** 1 settings", which is a count of
	// the answer rather than the answer. A tool promising the full config has
	// to be able to emit a configuration value.
	if strings.Contains(text, "settings") {
		t.Errorf("expected values rather than a per-section count, got: %s", text)
	}
}

// Two identical calls have to produce identical bytes. The renderer used to
// range over the config map directly, so section order changed per call and a
// reader had no way to tell a reordering from a change.
func TestConfigGet_NormalIsDeterministic(t *testing.T) {
	routes := map[string]any{
		"/config": map[string]any{
			"config": map[string]any{
				"webserver": map[string]any{"port": "80", "threads": 50},
				"dns":       map[string]any{"upstreams": []any{"8.8.8.8#53", "1.1.1.1"}},
				"dhcp":      map[string]any{"active": false},
				"misc":      map[string]any{},
			},
		},
	}

	first := callTool(t, configGetHandler, newTestClient(t, piholeHandler(routes)), nil)
	for i := range 8 {
		got := callTool(t, configGetHandler, newTestClient(t, piholeHandler(routes)), nil)
		if got != first {
			t.Fatalf("call %d rendered different bytes:\n%s\nwant:\n%s", i, got, first)
		}
	}

	want := "dhcp.active: false\n" +
		"dns.upstreams: [\"8.8.8.8#53\",\"1.1.1.1\"]\n" +
		"misc: {}\n" +
		"webserver.port: 80\n" +
		"webserver.threads: 50\n"
	if first != want {
		t.Errorf("rendered:\n%s\nwant:\n%s", first, want)
	}
}

func TestConfigGet_MinimalListsSectionsSorted(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config": map[string]any{
			"config": map[string]any{
				"webserver": map[string]any{"port": "80"},
				"dns":       map[string]any{"active": true},
				"dhcp":      map[string]any{"active": false},
			},
		},
	}))

	text := callTool(t, configGetHandler, c, map[string]any{"detail": "minimal"})
	if text != "Config sections: dhcp, dns, webserver" {
		t.Errorf("got %q, want the section names in sorted order", text)
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
	rec := piholeHandler(map[string]any{
		"PATCH /config": map[string]any{
			"config": map[string]any{"dns": map[string]any{"blocking": map[string]any{"active": false}}},
		},
	})
	c := newTestClient(t, rec)

	text := callTool(t, configSetHandler, c, map[string]any{
		"config": `{"dns":{"blocking":{"active":false}}}`,
	})
	if !strings.Contains(text, "Config updated") {
		t.Errorf("expected 'Config updated' message, got: %s", text)
	}

	// The Pi-hole API requires the body wrapped in a "config" key.
	// Regression: the handler previously sent the bare object, which the
	// API rejects with 400 "No \"config\" object in body data". The selector
	// pins the verb as well: the fake routes on path alone, so a handler that
	// switched to PUT would still be answered and the reply would look right.
	req := rec.Only(t, "PATCH", "/config")
	req.AssertBodyKeys(t, "config")
	req.AssertHeader(t, "Content-Type", "application/json")
	// The selector matches on the decoded path, which net/url has already
	// stripped the query from, so PATCH /config?restart=false satisfies it just
	// as happily. This tool rewrites the whole config; a stray restart=false
	// would defer the FTL reload the caller is expecting with no symptom in the
	// reply text.
	req.AssertNoQueryString(t)

	// Compare the whole wrapped object rather than the one leaf. FTL applies
	// what it is sent and answers 200 either way, so a handler that kept the
	// key under test while dropping its siblings would look successful and
	// write half the change.
	req.AssertField(t, "config", map[string]any{
		"dns": map[string]any{"blocking": map[string]any{"active": false}},
	})
	// Double-wrapping is the other way this write silently applies nothing, so
	// the happy path pins its absence too, not only the already-wrapped case.
	req.AssertNoField(t, "config.config")
}

func TestConfigSet_InvalidJSON(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{}))

	text := callToolExpectError(t, configSetHandler, c, map[string]any{
		"config": "{not valid json",
	})
	if !strings.Contains(text, "must be valid JSON") {
		t.Errorf("expected a JSON validation error, got: %s", text)
	}
}

// configSetRecorder answers the one PATCH the tool makes and keeps the request,
// so the assertions below read the wire rather than the canned reply.
func configSetRecorder() *recorder {
	return piholeHandler(map[string]any{
		"PATCH /config": map[string]any{"config": map[string]any{}},
	})
}

// A caller who worked around the missing wrapper sends {"config": {...}} already.
// Wrapping that again yields {"config":{"config":{...}}}, which FTL answers 200 to
// while applying nothing, so the envelope must be detected rather than nested.
func TestConfigSet_AlreadyWrappedPayloadIsNotDoubleWrapped(t *testing.T) {
	rec := configSetRecorder()
	c := newTestClient(t, rec)

	callTool(t, configSetHandler, c, map[string]any{
		"config": `{"config":{"dns":{"blocking":{"active":false}}}}`,
	})

	req := rec.Only(t, "PATCH", "/config")
	req.AssertBodyKeys(t, "config")
	req.AssertNoField(t, "config.config")
	req.AssertField(t, "config.dns.blocking.active", false)
}

// A bare object with a single "dns" key must still be wrapped normally.
func TestConfigSet_SingleKeyBarePayloadIsWrapped(t *testing.T) {
	rec := configSetRecorder()
	c := newTestClient(t, rec)

	callTool(t, configSetHandler, c, map[string]any{
		"config": `{"dns":{"blocking":{"active":false}}}`,
	})

	req := rec.Only(t, "PATCH", "/config")
	req.AssertBodyKeys(t, "config")
	req.AssertNoField(t, "config.config")
	req.AssertField(t, "config.dns.blocking.active", false)
}

func TestConfigSet_NonObjectJSON(t *testing.T) {
	for _, tc := range []struct{ name, config string }{
		{"array", "[1,2,3]"},
		{"number", "42"},
		{"string", `"dns"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, piholeHandler(map[string]any{}))
			text := callToolExpectError(t, configSetHandler, c, map[string]any{"config": tc.config})
			if !strings.Contains(text, "must be a JSON object") {
				t.Errorf("expected a JSON object error, got: %s", text)
			}
		})
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
	rec := piholeHandler(map[string]any{
		"/config/dns/upstreams": map[string]any{
			"config": map[string]any{
				"dns": map[string]any{"upstreams": []any{"1.1.1.1#53", "8.8.8.8#53"}},
			},
		},
	})
	c := newTestClient(t, rec)

	text := callTool(t, configGetValueHandler, c, map[string]any{"element": "dns.upstreams"})
	// The element is echoed back in the dotted spelling the caller used, not
	// the slash-separated path form it is turned into on the way out.
	if !strings.Contains(text, "dns.upstreams") {
		t.Errorf("expected element in output, got: %s", text)
	}
	if !strings.Contains(text, "1.1.1.1#53") {
		t.Errorf("expected upstream value, got: %s", text)
	}

	// Everything asserted above is built from the caller's own argument and the
	// canned reply, so none of it can see the request. This tool is the
	// read-back half of every write-then-read verification in
	// scripts/e2e-test.sh: if it ever addressed the wrong path or verb, those
	// verifications would stop verifying anything and stay green. Pin the verb,
	// the dotted-element-to-segment translation, and the absence of a body.
	req := rec.Only(t, "GET", "/config/dns/upstreams")
	req.AssertNoBody(t)
	req.AssertNoQueryString(t)
}

// 127.0.0.1#5335 is the canonical Unbound upstream, and the '#' used to end
// the path at the fragment: FTL saw /config/dns/upstreams/127.0.0.1, the
// appended restart parameter went into the discarded fragment with it, and the
// tool reported success for a value it had not written.
func TestConfigAddRemoveValue_ReservedCharactersAreEscaped(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(*pihole.Registry) server.ToolHandlerFunc
		method  string
	}{
		{"add", configAddValueHandler, "PUT"},
		{"remove", configRemoveValueHandler, "DELETE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Route-table keys are matched against the DECODED path, so the
			// key carries the literal '#'. The selector and AssertRawPath
			// below use the escaped spelling, which is what went on the wire.
			rec := piholeHandler(map[string]any{
				"/config/dns/upstreams/127.0.0.1#5335": map[string]any{},
			})
			c := newTestClient(t, rec)

			callTool(t, tc.handler, c, map[string]any{
				"element": "dns.upstreams",
				"value":   "127.0.0.1#5335",
				"restart": false,
			})

			req := rec.Only(t, tc.method, "/config/dns/upstreams/127.0.0.1%235335")
			req.AssertRawPath(t, "/config/dns/upstreams/127.0.0.1%235335")
			req.AssertQuery(t, "restart", "false")
		})
	}
}

func TestConfigAddValue_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns/upstreams/1.1.1.1": map[string]any{
			"config": map[string]any{
				"upstreams": []any{"1.1.1.1"},
			},
		},
	})
	c := newTestClient(t, rec)

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

	// This endpoint carries both the element and the new value in the path, so
	// the path is the whole request: the dotted element becomes segments and
	// the value is appended. PUT is what extends the array, and the reply text
	// is built from the tool's own arguments rather than the response, so
	// nothing above would notice a wrong verb or a mangled element.
	req := rec.Only(t, "PUT", "/config/dns/upstreams/1.1.1.1")
	// restart defaults to true and so does the API, so the flag belongs off the
	// wire. Sending restart=false here would quietly defer the FTL restart that
	// a caller who never mentioned it still expects.
	req.AssertNoQuery(t, "restart")
	req.AssertNoQueryString(t)
}

// restart=false has to reach the wire as a query parameter. The tool's reply is
// identical either way, so a dropped flag restarts FTL and interrupts DNS for
// every client on the network with no symptom a caller could see.
func TestConfigAddValue_RestartFalseIsSentAsQuery(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns/upstreams/1.1.1.1": map[string]any{
			"config": map[string]any{
				"upstreams": []any{"1.1.1.1"},
			},
		},
	})
	c := newTestClient(t, rec)

	callTool(t, configAddValueHandler, c, map[string]any{
		"element": "dns.upstreams",
		"value":   "1.1.1.1",
		"restart": false,
	})

	req := rec.Only(t, "PUT", "/config/dns/upstreams/1.1.1.1")
	req.AssertQuery(t, "restart", "false")
	// restart is the only parameter this endpoint takes, and a stray extra one
	// would be as invisible in the reply as a missing one.
	req.AssertQueryKeys(t, "restart")
}

func TestConfigRemoveValue_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns/upstreams/1.1.1.1": map[string]any{},
	})
	c := newTestClient(t, rec)

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

	// Add and remove differ only by verb: both address the same path and
	// neither sends a body. A DELETE that went out as a PUT would add the
	// value the caller asked to remove, and the reply would still say Removed.
	req := rec.Only(t, "DELETE", "/config/dns/upstreams/1.1.1.1")
	req.AssertNoQuery(t, "restart")
	req.AssertNoQueryString(t)
}

// The mirror of the add case: a silently dropped restart=false costs a
// network-wide DNS interruption that nothing in the reply hints at.
func TestConfigRemoveValue_RestartFalseIsSentAsQuery(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns/upstreams/1.1.1.1": map[string]any{},
	})
	c := newTestClient(t, rec)

	callTool(t, configRemoveValueHandler, c, map[string]any{
		"element": "dns.upstreams",
		"value":   "1.1.1.1",
		"restart": false,
	})

	req := rec.Only(t, "DELETE", "/config/dns/upstreams/1.1.1.1")
	req.AssertQuery(t, "restart", "false")
	req.AssertQueryKeys(t, "restart")
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

// A '+' is the trap inside the trap. url.PathEscape leaves it alone because a
// plus is a legal sub-delimiter in a path, but FTL decodes it as a space, so
// the escape has to be spelled out. Verified against FTL v6.7.
func TestConfigAddRemoveValue_EscapesAPlusInTheValue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(*pihole.Registry) server.ToolHandlerFunc
		method  string
	}{
		{"add", configAddValueHandler, "PUT"},
		{"remove", configRemoveValueHandler, "DELETE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := piholeHandler(map[string]any{
				"/config/dns/hosts/10.0.0.1+alias.lan": map[string]any{},
			})
			c := newTestClient(t, rec)

			callTool(t, tc.handler, c, map[string]any{
				"element": "dns.hosts",
				"value":   "10.0.0.1+alias.lan",
			})

			rec.Only(t, tc.method, "/config/dns/hosts/10.0.0.1+alias.lan").
				AssertRawPath(t, "/config/dns/hosts/10.0.0.1%2Balias.lan")
		})
	}
}

// The element is a dotted path, and the dots are separators: escaping the
// joined string turns every one of them into %2F and 404s the request. Each
// component is escaped on its own, so the separators survive and a component
// carrying a reserved character still travels intact.
func TestConfigValue_ElementSeparatorsSurviveEscaping(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns/upstreams": map[string]any{"config": map[string]any{"upstreams": []any{}}},
	})
	c := newTestClient(t, rec)

	callTool(t, configGetValueHandler, c, map[string]any{"element": "dns.upstreams"})

	rec.Only(t, "GET", "/config/dns/upstreams").AssertRawPath(t, "/config/dns/upstreams")
}

// unwrapConfigValue walks the response down to the leaf. Everything that does
// not match the shape FTL sends is handed back untouched rather than being
// reported as an empty value.
func TestUnwrapConfigValue(t *testing.T) {
	nested := map[string]any{"dns": map[string]any{"hosts": []any{"10.0.0.1 nas.lan"}}}

	for _, tt := range []struct {
		name    string
		cfg     map[string]any
		element string
		want    map[string]any
	}{
		{"nested section", nested, "dns.hosts", map[string]any{"hosts": []any{"10.0.0.1 nas.lan"}}},
		{"single component", map[string]any{"dns": "x"}, "dns", map[string]any{"dns": "x"}},
		{"component missing", nested, "dhcp.leases", nested},
		{"leaf is not an object", nested, "dns.hosts.deeper", nested},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := unwrapConfigValue(tt.cfg, tt.element)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("unwrapConfigValue(%v, %q) = %v, want %v", tt.cfg, tt.element, got, tt.want)
			}
		})
	}
}

// The section is user-supplied and goes straight into the path. A reserved
// character truncates the request, and the reply then describes a section
// nobody asked for.
func TestConfigGet_EscapesTheSection(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/config/dns#x": map[string]any{"config": map[string]any{"dns": map[string]any{}}},
	})
	c := newTestClient(t, rec)

	callTool(t, configGetHandler, c, map[string]any{"section": "dns#x"})

	rec.Only(t, "GET", "/config/dns#x").AssertRawPath(t, "/config/dns%23x")
}

// Unwrapping one envelope is not enough. A caller that wrapped twice, which is
// exactly what someone working around the original bug would do after reading
// that the payload needs a config key, reduced to a single wrap and was then
// re-wrapped on the way out. Pi-hole has no top-level config section, so a lone
// config key can only ever be the envelope, at any depth.
func TestConfigSet_DoublyWrappedPayloadIsNotSentWrapped(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"PATCH /config": map[string]any{"config": map[string]any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, configSetHandler, c, map[string]any{
		"config": `{"config":{"config":{"dns":{"cache":{"size":10001}}}}}`,
	})

	req := rec.Only(t, "PATCH", "/config")
	req.AssertField(t, "config.dns.cache.size", 10001)
	req.AssertNoField(t, "config.config")
}

// A config response with nothing in it must say so rather than render as an
// empty string, which a caller cannot distinguish from a failed read.
func TestConfigGet_NormalEmptyConfig(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/config": map[string]any{"config": map[string]any{}},
	}))

	if text := callTool(t, configGetHandler, c, nil); text != "No configuration returned." {
		t.Errorf("got %q, want a named empty result", text)
	}
}
