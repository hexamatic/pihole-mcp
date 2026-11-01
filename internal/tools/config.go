package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/format"
	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterConfig registers Pi-hole configuration tools.
func RegisterConfig(s *server.MCPServer, r *pihole.Registry) {
	addTool(s, r, mcp.NewTool("pihole_config_get",
		mcp.WithTitleAnnotation("Get Configuration"),
		mcp.WithDescription("Get Pi-hole configuration as dotted key/value lines. Name a section (dns, webserver, dhcp) for a subset, or omit for everything. detail=minimal lists section names; detail=full returns raw JSON."),
		mcp.WithString("section", mcp.Description("Config section: dns, webserver, dhcp, files, misc, etc.")),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), configGetHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_set",
		mcp.WithTitleAnnotation("Set Configuration"),
		mcp.WithDescription("Modify Pi-hole configuration. Provide nested JSON properties to change. Changes take effect immediately and can affect DNS behaviour system-wide."),
		mcp.WithString("config", mcp.Required(), mcp.Description("JSON config object, e.g. {\"dns\":{\"blocking\":{\"active\":true}}}")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true). Set false when chaining several config_set calls, so only the last one pays for the restart.")),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	), configSetHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_get_value",
		mcp.WithTitleAnnotation("Get Config Value"),
		mcp.WithDescription("Get a specific configuration value by dotted path (e.g. dns.upstreams, webserver.port, dhcp.active)."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams, dns.hosts (local DNS records) or dns.cnameRecords.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), configGetValueHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_add_value",
		mcp.WithTitleAnnotation("Add Config Array Value"),
		mcp.WithDescription("Add a value to a configuration array such as dns.upstreams. For dns.hosts and dns.cnameRecords prefer pihole_local_dns_add and pihole_local_cname_add, which take structured arguments."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams, dns.hosts or dns.cnameRecords.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to add.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithIdempotentHintAnnotation(true),
	), configAddValueHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_remove_value",
		mcp.WithTitleAnnotation("Remove Config Array Value"),
		mcp.WithDescription("Remove a value from a configuration array such as dns.upstreams. For dns.hosts and dns.cnameRecords prefer pihole_local_dns_delete and pihole_local_cname_delete, which take structured arguments."),
		mcp.WithString("element", mcp.Required(), mcp.Description("Config element path, e.g. dns.upstreams, dns.hosts or dns.cnameRecords.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to remove.")),
		mcp.WithBoolean("restart", mcp.Description("Restart FTL after change (default true).")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	), configRemoveValueHandler(r))

	addTool(s, r, mcp.NewTool("pihole_config_properties",
		mcp.WithTitleAnnotation("List Read-Only Config Keys"),
		mcp.WithDescription("List Pi-hole config keys that are read-only — set via pihole.toml or env var, not modifiable through the API. Use after a pihole_config_set error to confirm whether a key is intentionally locked."),
		formatParam,
		mcp.WithReadOnlyHintAnnotation(true),
	), configPropertiesHandler(r))
}

// credentialPaths are the configuration keys that hold password hashes.
//
// FTL masks webserver.api.password and webserver.api.totp_secret as "********"
// on read, but returns these two in full, and accepts writes to both through
// PATCH /api/config (measured against v6.7, 11 Sep 2026). So they are withheld
// from everything these tools return, and refused on the way in.
//
// Withheld means omitted, never replaced with a placeholder. pihole_config_set
// PATCHes whatever object it is given, and FTL leaves a key the PATCH does not
// mention exactly as it was, so a caller that reads a section and writes it
// back unchanged is safe. A placeholder in the same round trip would be
// written as the new hash and lock the admin out.
var credentialPaths = []string{"webserver.api.pwhash", "webserver.api.app_pwhash"}

// normaliseConfigPath reduces a dotted config path to the form credentialPaths
// uses, the one spelling of every path FTL may resolve to the same key.
func normaliseConfigPath(p string) string {
	return pihole.NormaliseConfigPath(p)
}

// isCredentialPath reports whether a config path names a password hash.
func isCredentialPath(p string) bool {
	n := normaliseConfigPath(p)
	for _, c := range credentialPaths {
		if n == c {
			return true
		}
	}
	return false
}

// withholdCredentials deletes the password hashes from a config tree as FTL
// returns it, rooted at the top level whatever section was asked for, and
// reports which it removed. Callers apply it before rendering in any form.
func withholdCredentials(cfg map[string]any) []string {
	var withheld []string
	for _, path := range credentialPaths {
		parts := strings.Split(path, ".")
		parent := cfg
		for _, seg := range parts[:len(parts)-1] {
			next, ok := parent[seg].(map[string]any)
			if !ok {
				parent = nil
				break
			}
			parent = next
		}
		if parent == nil {
			continue
		}
		if _, ok := parent[parts[len(parts)-1]]; ok {
			delete(parent, parts[len(parts)-1])
			withheld = append(withheld, path)
		}
	}
	return withheld
}

// withheldNote tells the caller a key exists but was left out, so it is not
// mistaken for a missing setting and "restored".
func withheldNote(withheld []string) string {
	if len(withheld) == 0 {
		return ""
	}
	return fmt.Sprintf("\n\n_Password hashes are never returned; withheld: `%s`._", strings.Join(withheld, "`, `"))
}

// credentialKeyIn returns the first password-hash key a config_set payload
// would write, or "". Keys are split on dots as well as nesting, because a
// payload may spell the same path either way.
func credentialKeyIn(obj map[string]any) string {
	var walk func(m map[string]any, prefix string) string
	walk = func(m map[string]any, prefix string) string {
		for k, v := range m {
			path := normaliseConfigPath(prefix + "." + k)
			if isCredentialPath(path) {
				return path
			}
			if child, ok := v.(map[string]any); ok {
				if found := walk(child, path); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(obj, "")
}

// refuseCredentialWrite is the error every write tool returns for a password
// hash. Whatever is written replaces the credential outright, and no caller of
// these tools can know a valid hash, so there is no legitimate write to make.
func refuseCredentialWrite(path string) *mcp.CallToolResult {
	if path == "webserver.api.app_pwhash" {
		return mcp.NewToolResultError("Refusing to write webserver.api.app_pwhash: it is the hash of the app password that integrations sign in with, and writing it replaces that password outright. Generate a new app password in Pi-hole's web interface instead.")
	}
	return mcp.NewToolResultError("Refusing to write " + path + ": it is the hash of the admin password, and writing it replaces that password outright. To change the password, set webserver.api.password to the new password and Pi-hole hashes it. That ends every open Pi-hole session, this server's included, so update PIHOLE_PASSWORD to match.")
}

func configGetHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path := "/config"
		if section := req.GetString("section", ""); section != "" {
			if err := validateMaxLength("section", section, maxConfigPathLen); err != nil {
				return mcp.NewToolResultError("Invalid " + err.Error()), nil
			}
			// A section is normally one component, but escapeConfigElement also
			// handles a dotted path without turning its separators into %2F.
			path += "/" + escapeConfigElement(section)
		}

		var result pihole.ConfigResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get config", err), nil
		}
		withheld := withholdCredentials(result.Config)

		detail := getDetail(req)

		if detail == "minimal" {
			sections := make([]string, 0, len(result.Config))
			for k := range result.Config {
				sections = append(sections, k)
			}
			sort.Strings(sections)
			return mcp.NewToolResultText(fmt.Sprintf("Config sections: %s", strings.Join(sections, ", "))), nil
		}

		if detail == "normal" {
			// One dotted line per setting. The old normal detail counted the
			// keys in each section and printed the counts, so a tool whose
			// description promises the full config could not emit a single
			// configuration value at the level callers get by default.
			flat := flattenTree(result.Config)
			if flat == "" {
				return mcp.NewToolResultText("No configuration returned."), nil
			}
			return mcp.NewToolResultText(flat + withheldNote(withheld)), nil
		}

		// full: JSON dump
		configJSON, err := json.MarshalIndent(result.Config, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to format config: %v", err)), nil
		}

		var b strings.Builder
		b.WriteString("```json\n")
		b.Write(configJSON)
		b.WriteString("\n```")
		b.WriteString(withheldNote(withheld))

		return mcp.NewToolResultText(b.String()), nil
	}
}

func configSetHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		configStr, err := req.RequireString("config")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'config' is required (JSON object)"), nil
		}

		// The Pi-hole API requires the update body wrapped in a "config" key
		// ({"config": {...}}). The tool parameter is the bare config object, so
		// wrap it before sending. Validate the JSON first so bad input surfaces
		// as a clear tool error rather than a 400 from the API.
		var configObj any
		if err := json.Unmarshal([]byte(configStr), &configObj); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Parameter 'config' must be valid JSON: %v", err)), nil
		}

		payload, ok := configObj.(map[string]any)
		if !ok {
			return mcp.NewToolResultError(`Parameter 'config' must be a JSON object, e.g. {"dns":{"blocking":{"active":true}}}`), nil
		}
		payload, err = expandConfigKeys(payload)
		if err != nil {
			return mcp.NewToolResultError("Parameter 'config': " + err.Error()), nil
		}

		// Accept a payload that already carries the API envelope. Pi-hole has no
		// top-level "config" section, so a lone "config" key can only be the
		// wrapper itself. Wrapping it a second time is the dangerous case: FTL
		// ignores keys it does not recognise and still answers 200, so the write
		// would silently apply nothing.
		// Unwrap as many envelopes as the caller supplied. Someone working
		// around the original bug by adding the key themselves can easily add
		// it to a payload that already had it, and one unwrap would then leave
		// a doubled envelope on the wire for FTL to ignore.
		for len(payload) == 1 {
			inner, wrapped := payload["config"].(map[string]any)
			if !wrapped {
				break
			}
			payload = inner
		}

		// Checked after unwrapping, so the test sees exactly the object FTL
		// would be sent, whichever envelope the caller used.
		if key := credentialKeyIn(payload); key != "" {
			return refuseCredentialWrite(key), nil
		}

		restart := req.GetBool("restart", true)
		path := "/config"
		if !restart {
			path += "?restart=false"
		}

		leaves := map[string]any{}
		configLeaves("", payload, leaves)
		if len(leaves) == 0 {
			return mcp.NewToolResultError(`Parameter 'config' names no setting to change, e.g. {"dns":{"blocking":{"active":true}}}`), nil
		}

		// A tool that removes one local DNS name writes back the whole
		// setting it read, so this write must not land in between.
		settings := make([]string, 0, len(leaves))
		for k := range leaves {
			settings = append(settings, k)
		}
		defer c.LockSettings(settings...)()

		// Read the current values and FTL's flags for them first. FTL skips
		// any key it does not recognise and still answers 200 with the whole
		// configuration, so without this a misspelt key was reported as
		// "Config updated". The flags say which settings make FTL restart.
		var before pihole.ConfigResponse
		beforeErr := c.Get(ctx, "/config?detailed=true", &before)

		if key := newlineIn(leaves); key != "" {
			return mcp.NewToolResultError(fmt.Sprintf("Refusing to write %s: its value contains a line break, which no Pi-hole "+
				"setting needs and which Pi-hole before v6.6 wrote into dnsmasq's configuration as a new option. Nothing was changed.",
				normaliseConfigPath(key))), nil
		}
		var current map[string]any
		if beforeErr == nil {
			current = before.Config
		}
		if key, r, ok := riskyChange(leaves, current); ok {
			return refuseRiskySetting(key, r), nil
		}

		sent := time.Now()
		var result pihole.ConfigResponse
		if err := c.Do(ctx, "PATCH", path, map[string]any{"config": payload}, &result); err != nil {
			return toolError("update config", err), nil
		}

		sendLog(ctx, mcp.LoggingLevelInfo, "config", map[string]any{"instance": c.Name(), "event": "config_updated"})

		if beforeErr != nil {
			return mcp.NewToolResultText(fmt.Sprintf("**Config updated.** Pi-hole accepted the change. This server could not "+
				"read the settings beforehand (%v), so it cannot say which of them changed or whether Pi-hole recognised "+
				"them all. Check with pihole_config_get_value.", beforeErr)), nil
		}

		report := compareConfigWrite(before.Config, result.Config, leaves)
		var b strings.Builder
		switch {
		case len(report.unknown) > 0:
			fmt.Fprintf(&b, "**Pi-hole does not recognise %s, so it ignored %s.** FTL skips unknown keys without an error. "+
				"Check the spelling against pihole_config_get.\n", strings.Join(report.unknown, ", "), pluralIt(len(report.unknown)))
		case len(report.changed) == 0:
			b.WriteString("**No setting changed.** Every value was already as requested.\n")
		default:
			b.WriteString("**Config updated.**\n")
		}
		if len(report.changed) > 0 {
			b.WriteString("Changed:\n")
			for _, ch := range report.changed {
				if ch.masked {
					fmt.Fprintf(&b, "- %s: updated (write-only, so Pi-hole does not show it)\n", ch.key)
					continue
				}
				fmt.Fprintf(&b, "- %s: %s → %s\n", ch.key, renderLeaf(ch.from), renderLeaf(ch.to))
			}
		}
		if len(report.unchanged) > 0 {
			fmt.Fprintf(&b, "Already set: %s\n", strings.Join(report.unchanged, ", "))
		}
		if report.endsSessions {
			b.WriteString("Pi-hole ends every API session when this setting changes, this server's included. It logs in again " +
				"on its next call using PIHOLE_PASSWORD, so if the admin password changed, update PIHOLE_PASSWORD to match.\n")
		}
		if report.restartsWithoutAsking || (restart && report.restarts) {
			b.WriteString(strings.TrimSpace(awaitRestartNote(ctx, c, sent)) + "\n")
		} else if report.restarts {
			b.WriteString("These changes take effect when FTL next restarts, since restart=false was passed.\n")
		}

		if len(report.unknown) > 0 {
			return mcp.NewToolResultError(b.String()), nil
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

// expandConfigKeys rewrites a config_set payload so that every key is a
// single path segment, nesting a dotted key such as "dns.blocking.active" the
// way its caller meant it. FTL reads only nested objects. It skips a dotted
// key as unknown and still answers 200, so such a write changed nothing and
// was reported as a success. FTL ignores case in these keys, so keys that
// differ only in case are merged as one, and naming one setting twice, or
// both as a setting and as a section, is an error rather than a guess.
func expandConfigKeys(in map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if err := mergeConfigKeys(out, in, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func mergeConfigKeys(dst, src map[string]any, prefix string) error {
	keys := make([]string, 0, len(src))
	for k := range src {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts := strings.Split(k, ".")
		node, path := dst, prefix
		for _, part := range parts[:len(parts)-1] {
			section, err := configSection(node, part, &path, k)
			if err != nil {
				return err
			}
			node = section
		}
		last := parts[len(parts)-1]
		if child, ok := src[k].(map[string]any); ok && len(child) > 0 {
			section, err := configSection(node, last, &path, k)
			if err != nil {
				return err
			}
			if err := mergeConfigKeys(section, child, path); err != nil {
				return err
			}
			continue
		}
		if err := addConfigSegment(&path, last, k); err != nil {
			return err
		}
		if _, _, found := pihole.ConfigChild(node, last); found {
			return fmt.Errorf("%s is set more than once", path)
		}
		node[last] = src[k]
	}
	return nil
}

// configSection returns the section named part inside node, creating it if
// it is not there yet, and extends path with it.
func configSection(node map[string]any, part string, path *string, key string) (map[string]any, error) {
	if err := addConfigSegment(path, part, key); err != nil {
		return nil, err
	}
	_, existing, found := pihole.ConfigChild(node, part)
	if !found {
		section := map[string]any{}
		node[part] = section
		return section, nil
	}
	section, ok := existing.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is given both a value and settings inside it", *path)
	}
	return section, nil
}

func addConfigSegment(path *string, part, key string) error {
	if strings.TrimSpace(part) == "" {
		return fmt.Errorf("key %q has an empty segment", key)
	}
	if *path != "" {
		*path += "."
	}
	*path += part
	return nil
}

// configLeaves flattens a config payload into dotted keys and the values set
// at them.
func configLeaves(prefix string, v any, out map[string]any) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		if prefix != "" {
			out[prefix] = v
		}
		return
	}
	for k, child := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		configLeaves(key, child, out)
	}
}

type configChange struct {
	key      string
	from, to any
	// masked is set for a write-only setting, whose values FTL never shows.
	masked bool
}

type configWriteReport struct {
	changed   []configChange
	unchanged []string
	unknown   []string
	// restarts is set when a changed setting makes FTL restart.
	restarts bool
	// restartsWithoutAsking is set when FTL restarts whatever restart says:
	// it always does when the privacy level is lowered.
	restartsWithoutAsking bool
	// endsSessions is set when a changed setting logs every session out.
	endsSessions bool
}

// maskedValue is how FTL reads back a write-only setting such as a password.
const maskedValue = "********"

// compareConfigWrite sorts the requested keys into changed, unchanged and
// unknown, from a detailed read taken before the write and the configuration
// FTL returned after it.
func compareConfigWrite(before, after map[string]any, leaves map[string]any) configWriteReport {
	var r configWriteReport
	keys := make([]string, 0, len(leaves))
	for k := range leaves {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, requested := range keys {
		item, ok := pihole.DetailedConfigItem(before, requested)
		if !ok {
			r.unknown = append(r.unknown, requested)
			continue
		}
		// Reported as FTL spells it, which can differ in case from the
		// request: FTL matches these keys ignoring case.
		key := item.Key
		now, _ := configValueAt(after, key)
		// FTL reads a write-only setting such as the password back as
		// "********" before and after, so a new value looks unchanged. Any
		// value other than the placeholder itself is a change.
		masked := item.Value == maskedValue && now == maskedValue && leaves[requested] != maskedValue
		if !masked && reflect.DeepEqual(item.Value, now) {
			r.unchanged = append(r.unchanged, key)
			continue
		}
		r.changed = append(r.changed, configChange{key: key, from: item.Value, to: now, masked: masked})
		if item.EndsSessions {
			r.endsSessions = true
		}
		if item.RestartsFTL {
			r.restarts = true
		}
		if key == "misc.privacylevel" {
			from, _ := item.Value.(float64)
			to, _ := now.(float64)
			if to < from {
				r.restartsWithoutAsking = true
			}
		}
	}
	return r
}

// configValueAt returns the value at a dotted key in a plain config tree,
// matching keys as FTL does.
func configValueAt(cfg map[string]any, key string) (any, bool) {
	var node any = cfg
	for _, part := range strings.Split(key, ".") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		if _, node, ok = pihole.ConfigChild(m, part); !ok {
			return nil, false
		}
	}
	return node, true
}

// configLock locks a config setting on c from a read to the write that
// depends on it, and returns the unlock. Every writer of a setting takes the
// same lock, however it spells the setting's path: these tools,
// pihole_config_set and pihole_instance_sync.
func configLock(c *pihole.Client, element string) func() {
	return c.LockSettings(element)
}

func pluralIt(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func configGetValueHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		element, err := req.RequireString("element")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'element' is required"), nil
		}
		if err := validateMaxLength("element", element, maxConfigPathLen); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		// Answered without a request: refusing after fetching would still
		// carry the hash across the wire into this process for nothing.
		if isCredentialPath(element) {
			return mcp.NewToolResultText(fmt.Sprintf("**%s:** withheld. Password hashes are never returned.", normaliseConfigPath(element))), nil
		}

		path := "/config/" + escapeConfigElement(element)

		var result pihole.ConfigResponse
		if err := c.Get(ctx, path, &result); err != nil {
			return toolError("get config value", err), nil
		}
		// A parent section (webserver, webserver.api) still carries the hashes,
		// and so does any spelling of the path the check above did not catch
		// but FTL resolved anyway.
		withheld := withholdCredentials(result.Config)

		// FTL nests the requested item under its full path from the root, so
		// dns.hosts comes back as {"config":{"dns":{"hosts":[...]}}}. The
		// element is a filter over what is included, never a change of root,
		// so walk down to the leaf before rendering it. Printing result.Config
		// straight out returns the wrapper the user did not ask for.
		value := unwrapConfigValue(result.Config, element)

		// Format the value — use JSON for complex types, plain text for scalars.
		var formatted string
		if len(value) == 1 {
			for _, v := range value {
				switch v.(type) {
				case string, float64, bool, nil:
					formatted = fmt.Sprintf("%v", v)
				default:
					j, _ := json.Marshal(v)
					formatted = string(j)
				}
			}
		} else {
			j, _ := json.Marshal(value)
			formatted = string(j)
		}

		return mcp.NewToolResultText(fmt.Sprintf("**%s:** %s", element, formatted) + withheldNote(withheld)), nil
	}
}

func configAddValueHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		element, err := req.RequireString("element")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'element' is required"), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'value' is required"), nil
		}
		if err := validateMaxLength("element", element, maxConfigPathLen); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		if err := validateMaxLength("value", value, maxCommentLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		if isCredentialPath(element) {
			return refuseCredentialWrite(normaliseConfigPath(element)), nil
		}
		if r, risky := riskySettingFor(element); risky {
			return refuseRiskySetting(element, r), nil
		}
		if hasLineBreak(value) {
			return mcp.NewToolResultError("Invalid value: it contains a line break, which no Pi-hole setting needs and which Pi-hole " +
				"before v6.6 wrote into dnsmasq's configuration as a new option."), nil
		}

		path := "/config/" + escapeConfigElement(element) + "/" + pihole.EscapePathSegment(value)

		restart := req.GetBool("restart", true)
		if !restart {
			path += "?restart=false"
		}
		defer configLock(c, element)()
		waits := restartsFor(ctx, c, element, restart)

		sent := time.Now()
		var result pihole.ConfigResponse
		if err := c.Put(ctx, path, nil, &result); err != nil {
			return toolError("add config value", err), nil
		}

		text := fmt.Sprintf("**Added** `%s` to `%s`.", value, element)
		if waits {
			text += awaitRestartNote(ctx, c, sent)
		}
		return mcp.NewToolResultText(text), nil
	}
}

func configRemoveValueHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		element, err := req.RequireString("element")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'element' is required"), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcp.NewToolResultError("Parameter 'value' is required"), nil
		}
		if err := validateMaxLength("element", element, maxConfigPathLen); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		if err := validateMaxLength("value", value, maxCommentLength); err != nil {
			return mcp.NewToolResultError("Invalid " + err.Error()), nil
		}
		if isCredentialPath(element) {
			return refuseCredentialWrite(normaliseConfigPath(element)), nil
		}
		if r, risky := riskySettingFor(element); risky {
			return refuseRiskySetting(element, r), nil
		}
		if hasLineBreak(value) {
			return mcp.NewToolResultError("Invalid value: it contains a line break, which no Pi-hole setting needs and which Pi-hole " +
				"before v6.6 wrote into dnsmasq's configuration as a new option."), nil
		}

		path := "/config/" + escapeConfigElement(element) + "/" + pihole.EscapePathSegment(value)

		restart := req.GetBool("restart", true)
		if !restart {
			path += "?restart=false"
		}
		defer configLock(c, element)()
		waits := restartsFor(ctx, c, element, restart)

		sent := time.Now()
		if err := c.Do(ctx, "DELETE", path, nil, nil); err != nil {
			return toolError("remove config value", err), nil
		}

		text := fmt.Sprintf("**Removed** `%s` from `%s`.", value, element)
		if waits {
			text += awaitRestartNote(ctx, c, sent)
		}
		return mcp.NewToolResultText(text), nil
	}
}

func configPropertiesHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, err := getInstance(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result pihole.ConfigPropertiesResponse
		if err := c.Get(ctx, "/config/_properties", &result); err != nil {
			return toolError("list config properties (requires Pi-hole FTL v6.6.1+)", err), nil
		}

		ros := result.Config.ReadOnly
		sort.Slice(ros, func(i, j int) bool { return ros[i].Key < ros[j].Key })
		if len(ros) == 0 {
			return mcp.NewToolResultText("No read-only config keys reported."), nil
		}

		if wantCSV(req) {
			headers := []string{"Key", "Reason", "Description"}
			rows := make([][]string, 0, len(ros))
			for _, p := range ros {
				rows = append(rows, []string{p.Key, p.Reason, p.Description})
			}
			return mcp.NewToolResultText(format.CSV(headers, rows)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%d read-only config keys:**\n", len(ros))
		for _, p := range ros {
			fmt.Fprintf(&b, "- `%s` (%s) — %s\n", p.Key, p.Reason, p.Description)
		}
		return mcp.NewToolResultText(b.String()), nil
	}
}

// unwrapConfigValue walks a config response down the dotted element path to
// the value the caller asked for. FTL builds the body from the root of the
// config tree, so a request for dns.hosts arrives wrapped in one object per
// path component. Anything that does not match that shape is returned as it
// came, so an unexpected body is still rendered rather than swallowed.
func unwrapConfigValue(cfg map[string]any, element string) map[string]any {
	out := cfg
	parts := strings.Split(element, ".")
	for i, p := range parts {
		v, ok := out[p]
		if !ok {
			return cfg
		}
		if i == len(parts)-1 {
			return map[string]any{p: v}
		}
		next, ok := v.(map[string]any)
		if !ok {
			return cfg
		}
		out = next
	}
	return out
}
