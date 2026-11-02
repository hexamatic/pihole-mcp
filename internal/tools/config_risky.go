package tools

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
)

// riskySetting is a Pi-hole setting the config tools will not change. Each
// can make Pi-hole run a program, or read or write a file of the caller's
// choosing, and each has been the way in for a published FTL advisory. A tool
// acting on text it has read, such as a queried domain or a log line, should
// not be able to reach them; they stay changeable in Pi-hole's web interface
// and pihole.toml.
type riskySetting struct {
	// path is a normalised dotted path. One ending in "." covers every
	// setting under it.
	path string
	why  string
}

var riskySettings = []riskySetting{
	{"misc.dnsmasq_lines", "it passes lines straight to dnsmasq, some of which run programs or read files (GHSA-ww5x-xx4x-qvjr)"},
	{"misc.etc_dnsmasq_d", "it makes dnsmasq load every file in /etc/dnsmasq.d"},
	{"webserver.advancedopts", "it passes options straight to the web server, some of which run programs (GHSA-2794-hrj8-5jg9)"},
	{"webserver.paths.", "it moves the files the web server serves, which has been used to read any file (GHSA-gx63-h4w6-f46g, GHSA-2h7g-g549-hf74)"},
	{"webserver.serve_all", "it makes the web server serve every file under its root (GHSA-2h7g-g549-hf74)"},
	{"webserver.tls.cert", "it points the web server at a file to read"},
	{"files.", "it moves Pi-hole's databases and logs to other files (GHSA-gx63-h4w6-f46g)"},
}

// riskySettingFor returns the entry covering a dotted config path, if any.
func riskySettingFor(path string) (riskySetting, bool) {
	n := normaliseConfigPath(path)
	for _, r := range riskySettings {
		if n == r.path || (strings.HasSuffix(r.path, ".") && (strings.HasPrefix(n, r.path) || n+"." == r.path)) {
			return r, true
		}
	}
	return riskySetting{}, false
}

func refuseRiskySetting(path string, r riskySetting) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("Refusing to change %s: %s. Nothing was changed. "+
		"Change it in Pi-hole's web interface or pihole.toml if you mean to.", normaliseConfigPath(path), r.why))
}

// riskyChange returns the first risky setting a config_set payload would
// change, judged against the detailed configuration read before the write.
// A setting written back with the value it already has is not a change, so a
// section read and written back whole still works. Without the earlier read
// (before is nil), any risky setting in the payload counts.
func riskyChange(leaves map[string]any, before map[string]any) (string, riskySetting, bool) {
	for key, value := range leaves {
		r, risky := riskySettingFor(key)
		if !risky {
			continue
		}
		if before != nil {
			if item, ok := pihole.DetailedConfigItem(before, key); ok && reflect.DeepEqual(item.Value, value) {
				continue
			}
		}
		return key, r, true
	}
	return "", riskySetting{}, false
}

// newlineIn returns the first config path whose value, or a string within
// it, contains a line break. FTL before v6.6 wrote several settings into
// dnsmasq's configuration as they were, so a line break started a new
// dnsmasq option (GHSA-23w8-7333-p9fj and its siblings). No setting needs one.
func newlineIn(leaves map[string]any) string {
	for key, value := range leaves {
		if hasLineBreak(value) {
			return key
		}
	}
	return ""
}

func hasLineBreak(v any) bool {
	switch v := v.(type) {
	case string:
		return strings.ContainsAny(v, "\r\n")
	case []any:
		for _, e := range v {
			if hasLineBreak(e) {
				return true
			}
		}
	}
	return false
}

// hasControl reports whether s holds a control character, NUL included. FTL
// reads config keys as C strings, so a key such as "dnsmasq_lines\x00x"
// reached FTL as dnsmasq_lines while matching none of the checks here, the
// risky settings and the password hashes alike. No config key contains one.
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
