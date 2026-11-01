package pihole

// errorResponse is the standard Pi-hole API error envelope.
type errorResponse struct {
	Error errorDetail `json:"error"`
	Took  float64     `json:"took"`
}

type errorDetail struct {
	Key     string `json:"key"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

// authRequest is the login request body.
type authRequest struct {
	Password string `json:"password"`
}

// authResponse is the login response envelope.
type authResponse struct {
	Session sessionInfo `json:"session"`
	Took    float64     `json:"took"`
}

type sessionInfo struct {
	Valid    bool   `json:"valid"`
	SID      string `json:"sid"`
	CSRF     string `json:"csrf"`
	Validity int    `json:"validity"`
	TOTP     bool   `json:"totp"`
	Message  string `json:"message"`
}

// BlockingStatus represents the DNS blocking state.
type BlockingStatus struct {
	Blocking string   `json:"blocking"`
	Timer    *float64 `json:"timer"`
	Took     float64  `json:"took"`
}

// BlockingRequest is the request body for changing blocking status.
type BlockingRequest struct {
	Blocking bool     `json:"blocking"`
	Timer    *float64 `json:"timer,omitempty"`
}

// StatsSummary is the response from GET /api/stats/summary.
type StatsSummary struct {
	Queries QueryStats  `json:"queries"`
	Clients ClientStats `json:"clients"`
	Gravity GravityInfo `json:"gravity"`
	Took    float64     `json:"took"`
}

// QueryStats contains query count breakdowns.
type QueryStats struct {
	Total          int            `json:"total"`
	Blocked        int            `json:"blocked"`
	PercentBlocked float64        `json:"percent_blocked"`
	UniqueDomains  int            `json:"unique_domains"`
	Forwarded      int            `json:"forwarded"`
	Cached         int            `json:"cached"`
	Frequency      float64        `json:"frequency"`
	Types          map[string]int `json:"types"`
	Status         map[string]int `json:"status"`
	Replies        map[string]int `json:"replies"`
}

// ClientStats contains client count information.
type ClientStats struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

// GravityInfo contains gravity blocklist information.
type GravityInfo struct {
	DomainsBeingBlocked int `json:"domains_being_blocked"`
	LastUpdate          int `json:"last_update"`
}

// TopItems is the response from top_domains and top_clients endpoints.
type TopItems struct {
	Domains        []TopDomain `json:"domains,omitempty"`
	Clients        []TopClient `json:"clients,omitempty"`
	TotalQueries   int         `json:"total_queries"`
	BlockedQueries int         `json:"blocked_queries"`
	Took           float64     `json:"took"`
}

// TopDomain is a single entry in the top domains list.
type TopDomain struct {
	Domain string `json:"domain"`
	Count  int    `json:"count"`
}

// TopClient is a single entry in the top clients list.
type TopClient struct {
	IP    string `json:"ip"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Upstreams is the response from GET /api/stats/upstreams.
type Upstreams struct {
	Upstreams        []Upstream `json:"upstreams"`
	ForwardedQueries int        `json:"forwarded_queries"`
	TotalQueries     int        `json:"total_queries"`
	Took             float64    `json:"took"`
}

// Upstream is a single upstream DNS server entry.
type Upstream struct {
	IP         *string            `json:"ip"`
	Name       *string            `json:"name"`
	Port       int                `json:"port"`
	Count      int                `json:"count"`
	Statistics UpstreamStatistics `json:"statistics"`
}

// UpstreamStatistics contains response time metrics for an upstream.
type UpstreamStatistics struct {
	Response float64 `json:"response"`
	Variance float64 `json:"variance"`
}

// QueryTypes is the response from GET /api/stats/query_types.
type QueryTypes struct {
	Types map[string]int `json:"types"`
	Took  float64        `json:"took"`
}

// RecentBlocked is the response from GET /api/stats/recent_blocked.
type RecentBlocked struct {
	Blocked []string `json:"blocked"`
	Took    float64  `json:"took"`
}

// DatabaseSummary is the response from GET /api/stats/database/summary.
type DatabaseSummary struct {
	SumQueries     int     `json:"sum_queries"`
	SumBlocked     int     `json:"sum_blocked"`
	PercentBlocked float64 `json:"percent_blocked"`
	TotalClients   int     `json:"total_clients"`
	Took           float64 `json:"took"`
}

// Domain represents a domain entry in Pi-hole's domain lists.
type Domain struct {
	Domain       string `json:"domain"`
	Unicode      string `json:"unicode"`
	Type         string `json:"type"`
	Kind         string `json:"kind"`
	Comment      string `json:"comment"`
	Groups       []int  `json:"groups"`
	Enabled      bool   `json:"enabled"`
	ID           int    `json:"id"`
	DateAdded    int    `json:"date_added"`
	DateModified int    `json:"date_modified"`
}

// DomainsResponse is the envelope for domain list responses.
type DomainsResponse struct {
	Domains   []Domain         `json:"domains"`
	Processed *ProcessedResult `json:"processed"`
	Took      float64          `json:"took"`
}

// ProcessedResult contains bulk operation results.
type ProcessedResult struct {
	Success []ProcessedItem `json:"success"`
	Errors  []ProcessedErr  `json:"errors"`
}

// ProcessedItem is a successfully processed item.
type ProcessedItem struct {
	Item string `json:"item"`
}

// ProcessedErr is a failed item with an error message.
type ProcessedErr struct {
	Item  string `json:"item"`
	Error string `json:"error"`
}

// Group represents a Pi-hole group.
type Group struct {
	Name         string `json:"name"`
	Comment      string `json:"comment"`
	Enabled      bool   `json:"enabled"`
	ID           int    `json:"id"`
	DateAdded    int    `json:"date_added"`
	DateModified int    `json:"date_modified"`
}

// GroupsResponse is the envelope for group list responses.
type GroupsResponse struct {
	Groups    []Group          `json:"groups"`
	Processed *ProcessedResult `json:"processed"`
	Took      float64          `json:"took"`
}

// ClientEntry represents a configured Pi-hole client.
type ClientEntry struct {
	Client       string `json:"client"`
	Name         string `json:"name"`
	Comment      string `json:"comment"`
	Groups       []int  `json:"groups"`
	ID           int    `json:"id"`
	DateAdded    int    `json:"date_added"`
	DateModified int    `json:"date_modified"`
}

// ClientsResponse is the envelope for client list responses.
type ClientsResponse struct {
	Clients   []ClientEntry    `json:"clients"`
	Processed *ProcessedResult `json:"processed"`
	Took      float64          `json:"took"`
}

// ClientSuggestion represents an unconfigured client seen by Pi-hole.
type ClientSuggestion struct {
	HWAddr    *string `json:"hwaddr"`
	MacVendor *string `json:"macVendor"`
	LastQuery int     `json:"lastQuery"`
	Addresses *string `json:"addresses"`
	Names     *string `json:"names"`
}

// ClientSuggestionsResponse is the envelope for client suggestions.
type ClientSuggestionsResponse struct {
	Clients []ClientSuggestion `json:"clients"`
	Took    float64            `json:"took"`
}

// List represents a blocklist/allowlist entry.
type List struct {
	Address        string `json:"address"`
	Type           string `json:"type"`
	Comment        string `json:"comment"`
	Groups         []int  `json:"groups"`
	Enabled        bool   `json:"enabled"`
	ID             int    `json:"id"`
	DateAdded      int    `json:"date_added"`
	DateModified   int    `json:"date_modified"`
	DateUpdated    int    `json:"date_updated"`
	Number         int    `json:"number"`
	InvalidDomains int    `json:"invalid_domains"`
	ABPEntries     int    `json:"abp_entries"`
	Status         int    `json:"status"`
}

// ListsResponse is the envelope for list responses.
type ListsResponse struct {
	Lists     []List           `json:"lists"`
	Processed *ProcessedResult `json:"processed"`
	Took      float64          `json:"took"`
}

// Query represents a single DNS query log entry.
type Query struct {
	ID       int         `json:"id"`
	Time     float64     `json:"time"`
	Type     string      `json:"type"`
	Domain   string      `json:"domain"`
	CName    *string     `json:"cname"`
	Status   string      `json:"status"`
	Client   QueryClient `json:"client"`
	DNSSEC   *string     `json:"dnssec"`
	Reply    QueryReply  `json:"reply"`
	ListID   *int        `json:"list_id"`
	Upstream *string     `json:"upstream"`
}

// QueryClient contains client info within a query.
type QueryClient struct {
	IP   string  `json:"ip"`
	Name *string `json:"name"`
}

// QueryReply contains reply info within a query.
type QueryReply struct {
	Type *string `json:"type"`
	Time float64 `json:"time"`
}

// QueriesResponse is the envelope for query log responses.
type QueriesResponse struct {
	Queries         []Query `json:"queries"`
	Cursor          int     `json:"cursor"`
	RecordsTotal    int     `json:"recordsTotal"`
	RecordsFiltered int     `json:"recordsFiltered"`
	// EarliestTimestamp and EarliestTimestampDisk are how far back FTL's
	// in-memory window and the on-disk long-term database reach. FTL returns
	// both on every query search, which is what lets an empty result say why
	// it is empty rather than just reporting zero. The disk figure is 0 until
	// queries have been flushed to disk, so treat zero as "not known yet".
	EarliestTimestamp     float64 `json:"earliest_timestamp,omitempty"`
	EarliestTimestampDisk float64 `json:"earliest_timestamp_disk,omitempty"`
	Took                  float64 `json:"took"`
}

// QuerySuggestions contains filter suggestions for the query log.
type QuerySuggestions struct {
	Suggestions SuggestionValues `json:"suggestions"`
	Took        float64          `json:"took"`
}

// SuggestionValues holds arrays of suggestion values per filter.
type SuggestionValues struct {
	Domain     []string `json:"domain"`
	ClientIP   []string `json:"client_ip"`
	ClientName []string `json:"client_name"`
	Upstream   []string `json:"upstream"`
	Type       []string `json:"type"`
	Status     []string `json:"status"`
	Reply      []string `json:"reply"`
	DNSSEC     []string `json:"dnssec"`
}

// SearchResponse is the response from GET /api/search/{domain}.
type SearchResponse struct {
	Search SearchData `json:"search"`
	Took   float64    `json:"took"`
}

// SearchData contains domain search results.
type SearchData struct {
	Domains    []SearchDomainResult  `json:"domains"`
	Gravity    []SearchGravityResult `json:"gravity"`
	Parameters SearchParameters      `json:"parameters"`
	Results    SearchResults         `json:"results"`
}

// SearchDomainResult is a domain list match.
type SearchDomainResult struct {
	Domain       string `json:"domain"`
	Comment      string `json:"comment"`
	Enabled      bool   `json:"enabled"`
	Type         string `json:"type"`
	Kind         string `json:"kind"`
	ID           int    `json:"id"`
	DateAdded    int    `json:"date_added"`
	DateModified int    `json:"date_modified"`
	Groups       []int  `json:"groups"`
}

// SearchGravityResult is a gravity list match.
type SearchGravityResult struct {
	Domain       string `json:"domain"`
	Address      string `json:"address"`
	Comment      string `json:"comment"`
	Enabled      bool   `json:"enabled"`
	Type         string `json:"type"`
	ID           int    `json:"id"`
	DateAdded    int    `json:"date_added"`
	DateModified int    `json:"date_modified"`
	DateUpdated  int    `json:"date_updated"`
	Number       int    `json:"number"`
	Status       int    `json:"status"`
	Groups       []int  `json:"groups"`
}

// SearchParameters contains the search parameters that were used.
type SearchParameters struct {
	Partial bool   `json:"partial"`
	N       int    `json:"N"`
	Domain  string `json:"domain"`
	Debug   bool   `json:"debug"`
}

// SearchResults contains result counts.
type SearchResults struct {
	Domains SearchResultCounts  `json:"domains"`
	Gravity SearchGravityCounts `json:"gravity"`
	Total   int                 `json:"total"`
}

// SearchResultCounts contains domain match counts.
type SearchResultCounts struct {
	Exact int `json:"exact"`
	Regex int `json:"regex"`
}

// SearchGravityCounts contains gravity match counts.
type SearchGravityCounts struct {
	Allow int `json:"allow"`
	Block int `json:"block"`
}

// HistoryResponse is the response from GET /api/history.
type HistoryResponse struct {
	History []HistoryEntry `json:"history"`
	Took    float64        `json:"took"`
}

// HistoryEntry is a single activity graph data point.
type HistoryEntry struct {
	Timestamp float64 `json:"timestamp"`
	Total     int     `json:"total"`
	Cached    int     `json:"cached"`
	Blocked   int     `json:"blocked"`
	Forwarded int     `json:"forwarded"`
}

// ClientHistoryResponse is the response from GET /api/history/clients and
// GET /api/history/database/clients.
type ClientHistoryResponse struct {
	Clients HistoryClients       `json:"clients"`
	History []ClientHistoryEntry `json:"history"`
	Took    float64              `json:"took"`
}

// ClientHistoryInfo contains client metadata in history responses.
type ClientHistoryInfo struct {
	Name *string `json:"name"`
	// Total is the client's query count. FTL v6.7's long-term client history
	// leaves it out, so nil means unknown rather than zero.
	Total *int `json:"total"`
}

// ClientHistoryEntry is a single per-client activity data point.
type ClientHistoryEntry struct {
	Timestamp float64        `json:"timestamp"`
	Data      map[string]int `json:"data"`
}

// SystemInfo is the response from GET /api/info/system.
type SystemInfo struct {
	System SystemDetails `json:"system"`
	Took   float64       `json:"took"`
}

// SystemDetails contains system resource information, as FTL v6.7 and v6.8
// send it (src/api/info.c, get_system_obj).
//
// The decoder this replaced read "perc", "load", "disk" and "dns", none of
// which FTL sends, so the tool reported 0% CPU, a load of zero, 0% disk and a
// stopped DNS resolver on every Pi-hole, and printed memory as bytes when FTL
// counts it in kilobytes. FTL reports no disk usage or resolver state at all.
type SystemDetails struct {
	// Uptime is the host's uptime in seconds.
	Uptime int        `json:"uptime"`
	Memory MemoryInfo `json:"memory"`
	// Procs is the number of processes running on the host.
	Procs int          `json:"procs"`
	CPU   CPUInfo      `json:"cpu"`
	FTL   ProcessUsage `json:"ftl"`
}

// MemoryInfo contains memory usage data.
type MemoryInfo struct {
	RAM  MemoryUsage `json:"ram"`
	Swap MemoryUsage `json:"swap"`
}

// MemoryUsage holds memory figures in KiB, as /proc/meminfo reports them
// (FTL labels them kB). Available is RAM only.
type MemoryUsage struct {
	Total       float64 `json:"total"`
	Free        float64 `json:"free"`
	Used        float64 `json:"used"`
	Available   float64 `json:"available"`
	PercentUsed float64 `json:"%used"`
}

// CPUInfo contains CPU usage data.
type CPUInfo struct {
	Nprocs      int     `json:"nprocs"`
	PercentUsed float64 `json:"%cpu"`
	Load        CPULoad `json:"load"`
}

// CPULoad holds the 1, 5 and 15 minute load averages, raw and as a
// percentage of the CPU count.
type CPULoad struct {
	Raw     [3]float64 `json:"raw"`
	Percent [3]float64 `json:"percent"`
}

// ProcessUsage is FTL's own share of the CPU and memory, in percent.
type ProcessUsage struct {
	CPU    float64 `json:"%cpu"`
	Memory float64 `json:"%mem"`
}

// HostInfo is the response from GET /api/info/host.
type HostInfo struct {
	Host HostDetails `json:"host"`
	Took float64     `json:"took"`
}

// HostDetails contains host information. FTL nests the uname fields; the
// decoder this replaced read them from the top level and so never found a
// host name, which hid the whole host line.
type HostDetails struct {
	Uname HostUname `json:"uname"`
	// Model is the board model where the kernel publishes one (Raspberry Pi
	// and other device-tree boards), and null elsewhere.
	Model *string `json:"model"`
}

// HostUname is the host's uname(2) record.
type HostUname struct {
	Sysname    string `json:"sysname"`
	Nodename   string `json:"nodename"`
	Release    string `json:"release"`
	Version    string `json:"version"`
	Machine    string `json:"machine"`
	Domainname string `json:"domainname"`
}

// SensorsInfo is the response from GET /api/info/sensors.
type SensorsInfo struct {
	Sensors SensorsData `json:"sensors"`
	Took    float64     `json:"took"`
}

// SensorsData contains sensor readings. FTL groups readings by hardware
// monitor, each with its own list of temperatures, and gives one unit for
// all of them.
type SensorsData struct {
	List []SensorGroup `json:"list"`
	// CPUTemp is FTL's pick of the CPU temperature, or null without one.
	CPUTemp *float64 `json:"cpu_temp"`
	// HotLimit is the temperature the web interface warns above.
	HotLimit float64 `json:"hot_limit"`
	// Unit is C, F or K, per webserver.api.temp.unit.
	Unit string `json:"unit"`
}

// SensorGroup is one hardware monitor (an hwmon device) and its readings.
type SensorGroup struct {
	Name   *string         `json:"name"`
	Path   string          `json:"path"`
	Source string          `json:"source"`
	Temps  []SensorReading `json:"temps"`
}

// SensorReading is a single temperature reading.
type SensorReading struct {
	Name   *string  `json:"name"`
	Value  float64  `json:"value"`
	Max    *float64 `json:"max"`
	Crit   *float64 `json:"crit"`
	Sensor string   `json:"sensor"`
}

// VersionInfo is the response from GET /api/info/version.
type VersionInfo struct {
	Version VersionDetails `json:"version"`
	Took    float64        `json:"took"`
}

// VersionDetails contains Pi-hole version information.
type VersionDetails struct {
	Core   VersionEntry `json:"core"`
	FTL    VersionEntry `json:"ftl"`
	Web    VersionEntry `json:"web"`
	Docker DockerInfo   `json:"docker"`
}

// VersionEntry contains version details for a component.
type VersionEntry struct {
	Local VersionLocal `json:"local"`
}

// VersionLocal contains local version info.
type VersionLocal struct {
	Version string `json:"version"`
	Branch  string `json:"branch"`
	Hash    string `json:"hash"`
}

// DockerInfo contains Docker tag information.
type DockerInfo struct {
	Local string `json:"local"`
}

// FTLInfo is the response from GET /api/info/ftl.
type FTLInfo struct {
	FTL  FTLDetails `json:"ftl"`
	Took float64    `json:"took"`
}

// FTLDetails contains FTL engine information.
type FTLDetails struct {
	PID              int            `json:"pid"`
	Uptime           float64        `json:"uptime"`
	Database         FTLdb          `json:"database"`
	PrivacyLevel     int            `json:"privacy_level"`
	QueryFrequency   float64        `json:"query_frequency"`
	Clients          FTLClients     `json:"clients"`
	MemPercent       float64        `json:"%mem"`
	CPUPercent       float64        `json:"%cpu"`
	AllowDestructive bool           `json:"allow_destructive"`
	Dnsmasq          map[string]any `json:"dnsmasq"`
}

// FTLClients holds FTL client population counts.
type FTLClients struct {
	Total  int `json:"total"`
	Active int `json:"active"`
}

// FTLdb contains FTL database content counts.
type FTLdb struct {
	Gravity int `json:"gravity"`
	Groups  int `json:"groups"`
	Lists   int `json:"lists"`
	Clients int `json:"clients"`
}

// DatabaseInfo is the response from GET /api/info/database.
//
// The Pi-hole v6 API returns the fields flat at the top level — no enclosing
// `database` key — so we decode directly into this struct. The `Unit` field
// is no longer present in v6 (size is always bytes); we keep it for
// completeness but it will be empty.
type DatabaseInfo struct {
	Size              float64 `json:"size"`
	Unit              string  `json:"unit,omitempty"`
	Queries           int     `json:"queries"`
	QueriesDisk       int     `json:"queries_disk,omitempty"`
	SQLite            string  `json:"sqlite_version"`
	EarliestTimestamp float64 `json:"earliest_timestamp,omitempty"`
	// EarliestTimestampDisk is how far back the on-disk long-term database
	// reaches. FTL reports 0 until queries have been flushed to disk, so a
	// caller has to treat zero as "not known yet" rather than "the epoch".
	EarliestTimestampDisk float64 `json:"earliest_timestamp_disk,omitempty"`
	Took                  float64 `json:"took"`
}

// MessagesResponse is the response from GET /api/info/messages.
type MessagesResponse struct {
	Messages []FTLMessage `json:"messages"`
	Took     float64      `json:"took"`
}

// FTLMessage is a single FTL diagnostic message.
//
// The text lives in "plain" — there is no "message" field, and the blob1..blob5
// columns are an internal detail of FTL's message table that the API does not
// expose. Reading the wrong key here is what left pihole_info_messages
// rendering a type and a timestamp with no text at all.
type FTLMessage struct {
	ID        int    `json:"id"`
	Timestamp int    `json:"timestamp"`
	Type      string `json:"type"`
	Plain     string `json:"plain"`
	HTML      string `json:"html"`
}

// ClientInfo is the response from GET /api/info/client.
type ClientInfo struct {
	RemoteAddr  string  `json:"remote_addr"`
	HTTPVersion string  `json:"http_version"`
	Method      string  `json:"method"`
	Took        float64 `json:"took"`
}

// ConfigResponse is the response from GET /api/config.
type ConfigResponse struct {
	Config map[string]any `json:"config"`
	Took   float64        `json:"took"`
}

// ConfigPropertiesResponse is the response from GET /api/config/_properties.
// Lists configuration keys that are read-only (set via pihole.toml or
// environment variable) and cannot be modified through the API. Added in
// FTL v6.6.1 — calls against earlier versions return 404.
type ConfigPropertiesResponse struct {
	Config struct {
		ReadOnly []ReadOnlyProperty `json:"read_only"`
	} `json:"config"`
	Took float64 `json:"took"`
}

// ReadOnlyProperty describes a single read-only config key.
type ReadOnlyProperty struct {
	Key         string `json:"key"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
}

// NetworkDevicesResponse is the response from GET /api/network/devices.
type NetworkDevicesResponse struct {
	Devices []NetworkDevice `json:"devices"`
	Took    float64         `json:"took"`
}

// NetworkDevice represents a device seen on the network.
type NetworkDevice struct {
	ID         int              `json:"id"`
	HWAddr     string           `json:"hwaddr"`
	Interface  string           `json:"interface"`
	FirstSeen  int              `json:"firstSeen"`
	LastQuery  int              `json:"lastQuery"`
	NumQueries int              `json:"numQueries"`
	MacVendor  *string          `json:"macVendor"`
	IPs        []NetworkAddress `json:"ips"`
}

// NetworkAddress is an IP/hostname associated with a network device.
type NetworkAddress struct {
	IP          string  `json:"ip"`
	Name        *string `json:"name"`
	LastSeen    int     `json:"lastSeen"`
	NameUpdated int     `json:"nameUpdated"`
}

// GatewayResponse is the response from GET /api/network/gateway.
type GatewayResponse struct {
	Gateway []GatewayEntry `json:"gateway"`
	Took    float64        `json:"took"`
}

// GatewayEntry is a single gateway entry.
type GatewayEntry struct {
	Family    string   `json:"family"`
	Interface string   `json:"interface"`
	Address   string   `json:"address"`
	Local     []string `json:"local"`
}

// NetworkRoutesResponse is the response from GET /api/network/routes.
type NetworkRoutesResponse struct {
	Routes []NetworkRoute `json:"routes"`
	Took   float64        `json:"took"`
}

// NetworkRoute is a single routing table entry.
type NetworkRoute struct {
	Family   string `json:"family"`
	Scope    string `json:"scope,omitempty"`
	Src      string `json:"src,omitempty"`
	Dst      string `json:"dst,omitempty"`
	Gateway  string `json:"gateway,omitempty"`
	Dev      string `json:"dev,omitempty"`
	OIF      string `json:"oif,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

// NetworkInterfacesResponse is the response from GET /api/network/interfaces.
type NetworkInterfacesResponse struct {
	Interfaces []NetworkInterface `json:"interfaces"`
	Took       float64            `json:"took"`
}

// NetworkInterface describes a network interface and its current state.
//
// Field names match the Pi-hole v6 response (see
// testdata/fixtures/network_interfaces.json). `Speed` is a pointer because
// the API returns null for interfaces without a meaningful link speed
// (loopback, tunl0).
type NetworkInterface struct {
	Name      string                 `json:"name"`
	Type      string                 `json:"type,omitempty"`
	State     string                 `json:"state,omitempty"`
	Speed     *int                   `json:"speed,omitempty"`
	Carrier   *bool                  `json:"carrier,omitempty"`
	Address   string                 `json:"address,omitempty"`
	Broadcast string                 `json:"broadcast,omitempty"`
	Flags     []string               `json:"flags,omitempty"`
	Addresses []NetworkInterfaceAddr `json:"addresses,omitempty"`
	Stats     *NetworkInterfaceStats `json:"stats,omitempty"`
}

// NetworkInterfaceAddr is an address bound to an interface.
type NetworkInterfaceAddr struct {
	Family    string `json:"family"`
	Address   string `json:"address"`
	PrefixLen int    `json:"prefixlen,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Local     string `json:"local,omitempty"`
}

// NetworkInterfaceStats holds traffic counters for an interface. The Pi-hole
// API returns rx_bytes / tx_bytes as objects with a unit suffix, not raw
// integers — see testdata/fixtures/network_interfaces.json.
type NetworkInterfaceStats struct {
	Bits    int                    `json:"bits,omitempty"`
	RxBytes *NetworkInterfaceValue `json:"rx_bytes,omitempty"`
	TxBytes *NetworkInterfaceValue `json:"tx_bytes,omitempty"`
}

// NetworkInterfaceValue is the {unit, value} envelope used by the
// network_interfaces stats fields.
type NetworkInterfaceValue struct {
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
}

// DHCPLeasesResponse is the response from GET /api/dhcp/leases.
type DHCPLeasesResponse struct {
	Leases []DHCPLease `json:"leases"`
	Took   float64     `json:"took"`
}

// DHCPLease represents an active DHCP lease.
type DHCPLease struct {
	Expires  int    `json:"expires"`
	Name     string `json:"name"`
	HWAddr   string `json:"hwaddr"`
	IP       string `json:"ip"`
	ClientID string `json:"clientid"`
}

// LogResponse is the response from log endpoints.
type LogResponse struct {
	Log    []LogEntry `json:"log"`
	NextID int        `json:"nextID"`
	Took   float64    `json:"took"`
}

// LogEntry is a single log line from Pi-hole.
type LogEntry struct {
	Timestamp float64 `json:"timestamp"`
	Message   string  `json:"message"`
	Priority  *string `json:"prio"`
}

// ActionResponse is the response from action endpoints.
type ActionResponse struct {
	Success bool    `json:"success"`
	Took    float64 `json:"took"`
}

// TeleporterImportResponse is the response from POST /api/teleporter.
//
// FTL reports what it imported under "files" (src/api/teleporter.c in v6.7
// and v6.8 alike). Its own OpenAPI spec documents "processed", which is the
// key this struct used to read, so every import rendered an empty list and
// read the same whether Pi-hole restored everything or nothing. Processed is
// kept in case a future FTL brings the code in line with its documentation.
type TeleporterImportResponse struct {
	Files     []string `json:"files"`
	Processed []string `json:"processed"`
	Took      float64  `json:"took"`
}

// ImportedFiles returns what Pi-hole reported importing, from whichever key
// carried it.
func (r TeleporterImportResponse) ImportedFiles() []string {
	if len(r.Files) > 0 {
		return r.Files
	}
	return r.Processed
}

// MetricsInfo is the response from GET /api/info/metrics.
type MetricsInfo struct {
	Metrics map[string]any `json:"metrics"`
	Took    float64        `json:"took"`
}

// SessionsResponse is the response from GET /api/auth/sessions.
type SessionsResponse struct {
	Sessions []Session `json:"sessions"`
	Took     float64   `json:"took"`
}

// Session represents an active API session.
type Session struct {
	ID         int     `json:"id"`
	RemoteAddr string  `json:"remote_addr"`
	UserAgent  string  `json:"user_agent"`
	ValidUntil float64 `json:"valid_until"`
	// FTL names this "current_session" (src/api/auth.c, v6.7 and v6.8). It
	// was read as "this", so no session was ever marked as this server's.
	CurrentSession bool `json:"current_session"`
}
