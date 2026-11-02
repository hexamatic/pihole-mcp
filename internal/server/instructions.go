package server

import (
	"regexp"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/toolsets"
)

// Instructions are injected into every conversation, so a sentence that is
// wrong for a deployment is wrong in every turn of it.
//
// The split matters for read-only mode. Five of these sentences tell the model
// to call a tool that read-only mode removes. Leaving them in place and
// appending "but this server is read-only" ships text that contradicts itself,
// and a model that follows the earlier sentence pays a rejected call to find
// out. Removing them is the honest form.
var (
	baseInstructionSentences = []string{
		"Pi-hole v6 DNS management server.",
		"Start with pihole_padd for a one-call dashboard (queries, blocking, top domain/client, cache, versions, host health); use pihole_stats_summary when you need query detail.",
		"Use pihole_queries_suggestions to discover valid filter values for pihole_queries_search.",
		"For time-range queries, use pihole_stats_database_* tools with from/until timestamps.",
		"pihole_info_ftl provides dnsmasq-internal metrics not available in pihole_info_system.",
		"Use pihole_local_dns_list and pihole_local_cname_list to see the records this Pi-hole answers for locally.",
		// Reworded from "Tools accept optional...": 15 tools take detail and 20
		// take format, so the unconditional claim was false for most of the
		// surface and was asserted in every conversation.
		"Some tools accept optional 'detail' (minimal/normal/full) and 'format' (text/csv) parameters.",
	}

	writeInstructionSentences = []string{
		"Use pihole_search_domains before pihole_domains_add to check for duplicates.",
		"After pihole_lists_add or pihole_lists_delete, run pihole_action_gravity_update to apply changes.",
		"When adding multiple upstream DNS servers, use pihole_config_add_value with restart=false for all but the last change.",
		"If a pihole_config_set call is rejected for a single key, run pihole_config_properties to confirm whether pihole.toml or an environment variable locks it. If every configuration write is refused with a 403 that names webserver.api.app_sudo, the server is using an application password, which needs that setting turned on to change configuration.",
		"Recent Pi-hole FTL keys settable via pihole_config_set include resolver.macNames (FTL v6.6, MAC-based hostname resolution), database.forceDisk (v6.5, keeps the query history on disk to save RAM, at the cost of slower history browsing, especially on SD cards), and dns.cache.rrtype (v6.5, per-RR-type caching).",
	}

	readOnlyInstructionSentence = "This deployment is read-only: every tool that changes Pi-hole is unavailable, and calling one is rejected."
)

// instructions builds the server instruction string for a deployment.
// selected is the resolved PIHOLE_TOOLSETS selection, nil for every toolset.
//
// A sentence that names a tool is kept only when every tool it names belongs
// to a selected toolset. The instructions used to name tools whatever the
// selection, so a deployment narrowed to the DNS toolset was told in every
// conversation to start with pihole_padd, which it did not have.
func instructions(readOnly bool, selected map[string]bool) string {
	candidates := baseInstructionSentences
	if !readOnly {
		candidates = append(candidates[:len(candidates):len(candidates)], writeInstructionSentences...)
	}
	sentences := make([]string, 0, len(candidates)+1)
	for _, s := range candidates {
		if namesOnlySelectedTools(s, selected) {
			sentences = append(sentences, s)
		}
	}
	if readOnly {
		sentences = append(sentences, readOnlyInstructionSentence)
	}
	return strings.Join(sentences, " ")
}

// toolNamePattern finds tool names in a sentence, a trailing "*" included
// for a family such as pihole_stats_database_*.
var toolNamePattern = regexp.MustCompile(`pihole_[a-z_]+\*?`)

// namesOnlySelectedTools reports whether every tool a sentence names belongs
// to a selected toolset. Toolset membership follows from the name alone
// (toolsets.Of), so this needs no registered tools and keeps instructions
// network-free for cmd/toolsdoc.
func namesOnlySelectedTools(sentence string, selected map[string]bool) bool {
	if selected == nil {
		return true
	}
	for _, name := range toolNamePattern.FindAllString(sentence, -1) {
		if set, ok := toolsets.Of(name); !ok || !selected[set] {
			return false
		}
	}
	return true
}
