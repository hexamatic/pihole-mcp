package server

import "strings"

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
		"If a pihole_config_set call is rejected as read-only, run pihole_config_properties to confirm which keys are locked by pihole.toml or env vars.",
		"Recent Pi-hole FTL keys settable via pihole_config_set include resolver.macNames (FTL v6.6, MAC-based hostname resolution), database.forceDisk (v6.5, lower RAM use), and dns.cache.rrtype (v6.5, per-RR-type caching).",
	}

	readOnlyInstructionSentence = "This deployment is read-only: every tool that changes Pi-hole is unavailable, and calling one is rejected."
)

// instructions builds the server instruction string for a deployment.
func instructions(readOnly bool) string {
	sentences := make([]string, 0, len(baseInstructionSentences)+len(writeInstructionSentences)+1)
	sentences = append(sentences, baseInstructionSentences...)
	if readOnly {
		sentences = append(sentences, readOnlyInstructionSentence)
	} else {
		sentences = append(sentences, writeInstructionSentences...)
	}
	return strings.Join(sentences, " ")
}
