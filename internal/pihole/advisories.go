package pihole

import (
	"fmt"
	"strconv"
	"strings"
)

// FTLVersion is a release version of Pi-hole FTL, such as v6.7.1.
type FTLVersion struct {
	Major, Minor, Patch int
}

// ParseFTLVersion reads a release version as FTL reports it ("v6.8",
// "v6.7.1"). A development build ("vDev-1a2b3c4") or anything else that is
// not a release reads as not ok, since its fixes cannot be told from its name.
func ParseFTLVersion(s string) (FTLVersion, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) < 2 || len(parts) > 3 {
		return FTLVersion{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return FTLVersion{}, false
		}
		n[i] = v
	}
	return FTLVersion{n[0], n[1], n[2]}, true
}

// Before reports whether v is an earlier release than o.
func (v FTLVersion) Before(o FTLVersion) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

func (v FTLVersion) String() string {
	if v.Patch == 0 {
		return fmt.Sprintf("v%d.%d", v.Major, v.Minor)
	}
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Advisory is a published Pi-hole FTL security advisory.
type Advisory struct {
	ID       string
	Severity string // low, medium, high or critical, as published
	FixedIn  FTLVersion
	Summary  string
}

// ftlAdvisories are the security advisories published for pi-hole/FTL, as
// listed by GitHub's advisory API on 2 November 2026, newest fix first. A
// Pi-hole running a release before an advisory's FixedIn is affected by it.
// The list is extended when FTL publishes more; a release that fixes them
// all simply matches none.
var ftlAdvisories = []Advisory{
	{"GHSA-2h7g-g549-hf74", "high", FTLVersion{6, 8, 0}, "Authenticated File Read and Fix Bypass"},
	{"GHSA-322h-f873-hp54", "low", FTLVersion{6, 8, 0}, "Any website can read and change a Pi-hole v6 without a password through a LAN user's browser (CORS '*' + CSRF + DNS rebinding)"},
	{"GHSA-8fc6-q9x4-892v", "medium", FTLVersion{6, 8, 0}, "Unauthenticated CHAOS/TXT DNS queries overflow pihole-FTL's shared stat buffer (DNS denial of service)"},
	{"GHSA-9828-2f2q-2c8v", "low", FTLVersion{6, 8, 0}, "authenticated use-after-free in the domain search API during a list reload crashes the resolver"},
	{"GHSA-mqr7-j2p5-5495", "low", FTLVersion{6, 8, 0}, "authenticated double free in the domain-list regex validation API crashes the resolver"},
	{"GHSA-v53r-4564-3449", "medium", FTLVersion{6, 8, 0}, "App-password and CLI sessions can read all admin auth secrets"},
	{"GHSA-v8mj-cqgw-63rj", "low", FTLVersion{6, 8, 0}, "Use-after-free of DHCP leases: DELETE /api/dhcp/leases frees a lease on a worker thread while dnsmasq's main thread prunes the same list"},
	{"GHSA-xj4x-gf2g-r6m7", "medium", FTLVersion{6, 8, 0}, "an EDNS option from one query can be applied to another client's query"},
	{"GHSA-2794-hrj8-5jg9", "high", FTLVersion{6, 7, 1}, "pihole-FTL: webserver.advancedOpts RCE -> root + arbitrary file read"},
	{"GHSA-7997-2868-rjfg", "medium", FTLVersion{6, 7, 1}, "stack buffer overflow in _FTL_make_answer(): the question name buffer is sized for the wire form, not for the escaped representation extract_name() writes into it"},
	{"GHSA-gx63-h4w6-f46g", "high", FTLVersion{6, 7, 1}, "Configuration validators can be bypassed via Teleporter import, combined changes and document-root relocation"},
	{"GHSA-ww5x-xx4x-qvjr", "high", FTLVersion{6, 7, 1}, "misc.dnsmasq_lines accepts exec-capable options -> remote code execution"},
	{"GHSA-8j7w-m3cr-6q6x", "high", FTLVersion{6, 7, 0}, "Remote Code Execution (RCE) via CivetWeb Configuration Injection"},
	{"GHSA-g7v8-8q8f-hprp", "medium", FTLVersion{6, 7, 0}, "Log injection in webserver access-log writer chains to remote code execution via Lua-server-page evaluation"},
	{"GHSA-q6fm-xwxf-37r5", "high", FTLVersion{6, 7, 0}, "WebUI (& API) DoS mechanism - Lack of rate limiting : Silent CPU consumption and/or Log file expansion"},
	{"GHSA-r5vh-5q82-jg7q", "low", FTLVersion{6, 7, 0}, "CRLF Injection / HTTP Header Injection via Group Name"},
	{"GHSA-w8cr-2cwg-92cg", "high", FTLVersion{6, 7, 0}, "Session Expiration Bypass"},
	{"GHSA-9cqv-839p-gpq2", "high", FTLVersion{6, 6, 1}, "Newline Injection → RCE via dnsmasq dhcp-script"},
	{"GHSA-9ff5-f3v5-2xc7", "high", FTLVersion{6, 6, 1}, "Unauthenticated Session Hijacking via Race Condition on Global Session Buffer"},
	{"GHSA-23w8-7333-p9fj", "high", FTLVersion{6, 6, 0}, "Remote Code Execution (RCE) via dns.upstreams Newline Injection"},
	{"GHSA-28g5-gg88-wh5m", "high", FTLVersion{6, 6, 0}, "Remote Code Execution (RCE) via dns.cnameRecords Newline Injection"},
	{"GHSA-fqv2-qhfh-ghcj", "high", FTLVersion{6, 6, 0}, "Remote Code Execution (RCE) via dhcp.leaseTime Newline Injection"},
	{"GHSA-r7g8-3fj7-m5qq", "medium", FTLVersion{6, 6, 0}, "Authorization bypass: CLI API sessions can import Teleporter archives and modify configuration"},
	{"GHSA-vfmq-jrx3-wv3c", "high", FTLVersion{6, 6, 0}, "Remote Code Execution (RCE) via dhcp.hosts Newline Injection"},
	{"GHSA-wxhv-w77q-6qwp", "high", FTLVersion{6, 6, 0}, "Remote Code Execution (RCE) via dns.hostRecord Newline Injection"},
	{"GHSA-667h-gpmh-7gxc", "medium", FTLVersion{6, 5, 0}, "Authenticated Arbitrary File Read via `file://` Blocklists in Pi-hole (FTL / Web v6)"},
}

// AdvisoriesFor returns the published advisories that affect FTL release v,
// newest fix first.
func AdvisoriesFor(v FTLVersion) []Advisory {
	var out []Advisory
	for _, a := range ftlAdvisories {
		if v.Before(a.FixedIn) {
			out = append(out, a)
		}
	}
	return out
}

// severityRank orders severities for picking the worst.
var severityRank = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}

// AdvisoryWarning is one sentence saying how many published advisories
// affect the FTL release named by version, the worst severity among them,
// and the release that fixes them all, or "" when none do or the version is
// not a release.
func AdvisoryWarning(version string) string {
	v, ok := ParseFTLVersion(version)
	if !ok {
		return ""
	}
	affecting := AdvisoriesFor(v)
	if len(affecting) == 0 {
		return ""
	}
	worst, fixedBy := affecting[0], affecting[0].FixedIn
	for _, a := range affecting {
		if severityRank[a.Severity] > severityRank[worst.Severity] {
			worst = a
		}
		if fixedBy.Before(a.FixedIn) {
			fixedBy = a.FixedIn
		}
	}
	noun := "advisories"
	if len(affecting) == 1 {
		noun = "advisory"
	}
	return fmt.Sprintf("FTL %s is affected by %d published security %s, the worst rated %s (%s); FTL %s fixes them. "+
		"Update Pi-hole with pihole -up, or pull a newer Docker image.", v, len(affecting), noun, worst.Severity, worst.ID, fixedBy)
}
