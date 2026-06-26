package tools

// AggregateSummary holds the success/failure counts for an instance=all call.
type AggregateSummary struct {
	Total  int `json:"total" jsonschema:"Number of instances queried"`
	OK     int `json:"ok" jsonschema:"Number of instances that returned successfully"`
	Failed int `json:"failed" jsonschema:"Number of instances that failed"`
}

// AggregateInstanceResult is one instance's contribution to an instance=all
// aggregate. Data carries the instance's structured result when the underlying
// tool produced one; Text always carries the human-readable result.
type AggregateInstanceResult struct {
	Instance string `json:"instance" jsonschema:"Name of the Pi-hole instance this result came from"`
	OK       bool   `json:"ok" jsonschema:"True when the call against this instance succeeded"`
	Error    string `json:"error,omitempty" jsonschema:"Error message when ok is false"`
	Data     any    `json:"data,omitempty" jsonschema:"The instance's structured result, when the tool produced one"`
	Text     string `json:"text,omitempty" jsonschema:"The instance's text result"`
}

// AggregateOutput is the structured output returned when a read-only tool is
// called with instance=all. It labels every record with its source instance so
// a model can reliably attribute data without inferring provenance from order.
type AggregateOutput struct {
	Summary   AggregateSummary          `json:"summary" jsonschema:"Counts across all queried instances"`
	Instances []AggregateInstanceResult `json:"instances" jsonschema:"Per-instance results, in configured declaration order"`
}

// BlockingStatusOutput is the structured output for pihole_dns_get_blocking.
type BlockingStatusOutput struct {
	Blocking string   `json:"blocking" jsonschema:"Current blocking state: enabled or disabled"`
	Timer    *float64 `json:"timer,omitempty" jsonschema:"Seconds remaining until automatic state revert"`
}

// StatsSummaryOutput is the structured output for pihole_stats_summary.
type StatsSummaryOutput struct {
	TotalQueries     int     `json:"total_queries" jsonschema:"Total DNS queries processed"`
	BlockedQueries   int     `json:"blocked_queries" jsonschema:"Number of blocked queries"`
	PercentBlocked   float64 `json:"percent_blocked" jsonschema:"Percentage of queries blocked"`
	CachedQueries    int     `json:"cached_queries" jsonschema:"Number of cached queries"`
	ForwardedQueries int     `json:"forwarded_queries" jsonschema:"Number of forwarded queries"`
	ActiveClients    int     `json:"active_clients" jsonschema:"Number of active clients"`
	TotalClients     int     `json:"total_clients" jsonschema:"Total known clients"`
	GravityDomains   int     `json:"gravity_domains" jsonschema:"Number of domains in gravity blocklist"`
}

// PADDOutput is the structured output for pihole_padd — the consolidated
// dashboard snapshot.
type PADDOutput struct {
	Blocking       string  `json:"blocking" jsonschema:"Current blocking state: enabled or disabled"`
	TotalQueries   int     `json:"total_queries" jsonschema:"Total DNS queries processed today"`
	BlockedQueries int     `json:"blocked_queries" jsonschema:"Number of blocked queries today"`
	PercentBlocked float64 `json:"percent_blocked" jsonschema:"Percentage of queries blocked"`
	QueryFrequency float64 `json:"query_frequency" jsonschema:"Recent queries per second"`
	ActiveClients  int     `json:"active_clients" jsonschema:"Number of active clients"`
	GravitySize    int     `json:"gravity_size" jsonschema:"Number of domains in gravity blocklist"`
	TopDomain      string  `json:"top_domain,omitempty" jsonschema:"Most queried permitted domain"`
	TopBlocked     string  `json:"top_blocked,omitempty" jsonschema:"Most blocked domain"`
	TopClient      string  `json:"top_client,omitempty" jsonschema:"Most active client"`
	RecentBlocked  string  `json:"recent_blocked,omitempty" jsonschema:"Most recently blocked domain"`
	CacheSize      int     `json:"cache_size" jsonschema:"DNS cache size"`
	CPUPercent     float64 `json:"cpu_percent" jsonschema:"FTL process CPU usage percentage"`
	MemPercent     float64 `json:"mem_percent" jsonschema:"FTL process memory usage percentage"`
	CoreVersion    string  `json:"core_version,omitempty" jsonschema:"Pi-hole core version"`
	FTLVersion     string  `json:"ftl_version,omitempty" jsonschema:"FTL engine version"`
	WebVersion     string  `json:"web_version,omitempty" jsonschema:"Web interface version"`
	NodeName       string  `json:"node_name,omitempty" jsonschema:"Host name of the Pi-hole node"`
}

// InfoSystemOutput is the structured output for pihole_info_system.
type InfoSystemOutput struct {
	Hostname    string  `json:"hostname,omitempty" jsonschema:"Host name"`
	Uptime      int     `json:"uptime_seconds" jsonschema:"System uptime in seconds"`
	LoadAverage float64 `json:"load_average" jsonschema:"1-minute load average"`
	CPUCores    int     `json:"cpu_cores" jsonschema:"Number of CPU cores"`
	CPUPercent  float64 `json:"cpu_percent" jsonschema:"CPU usage percentage"`
	MemoryUsed  float64 `json:"memory_used" jsonschema:"Used memory in the reported unit"`
	MemoryTotal float64 `json:"memory_total" jsonschema:"Total memory in the reported unit"`
	MemoryUnit  string  `json:"memory_unit,omitempty" jsonschema:"Unit for the memory values (e.g. B, KiB, MiB)"`
	MemoryPerc  float64 `json:"memory_percent" jsonschema:"Memory usage percentage"`
	DiskPercent float64 `json:"disk_percent" jsonschema:"Disk usage percentage"`
	DNSRunning  bool    `json:"dns_running" jsonschema:"Whether the DNS resolver is running"`
}

// TopItemOutput represents a single ranked item (domain or client).
type TopItemOutput struct {
	Name  string `json:"name" jsonschema:"Domain name or client identifier"`
	Count int    `json:"count" jsonschema:"Query count"`
}

// TopListOutput is the structured output for pihole_stats_top_domains and
// pihole_stats_top_clients.
type TopListOutput struct {
	Items   []TopItemOutput `json:"items" jsonschema:"Ranked list of items, highest count first"`
	Count   int             `json:"count" jsonschema:"Number of items returned"`
	Blocked bool            `json:"blocked" jsonschema:"True when ranking by blocked queries"`
}

// DomainOutput represents a single domain in structured output.
type DomainOutput struct {
	Domain  string `json:"domain" jsonschema:"Domain name or regex pattern"`
	Type    string `json:"type" jsonschema:"allow or deny"`
	Kind    string `json:"kind" jsonschema:"exact or regex"`
	Enabled bool   `json:"enabled" jsonschema:"Whether the entry is active"`
	Comment string `json:"comment,omitempty" jsonschema:"Optional comment"`
}

// DomainsListOutput is the structured output for pihole_domains_list.
type DomainsListOutput struct {
	Domains []DomainOutput `json:"domains" jsonschema:"List of domain entries"`
	Count   int            `json:"count" jsonschema:"Total number of domains"`
}
