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
	Took            float64 `json:"took"`
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

// ClientHistoryResponse is the response from GET /api/history/clients.
type ClientHistoryResponse struct {
	Clients map[string]ClientHistoryInfo `json:"clients"`
	History []ClientHistoryEntry         `json:"history"`
	Took    float64                      `json:"took"`
}

// ClientHistoryInfo contains client metadata in history responses.
type ClientHistoryInfo struct {
	Name  *string `json:"name"`
	Total int     `json:"total"`
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

// SystemDetails contains system resource information.
type SystemDetails struct {
	Uptime int            `json:"uptime"`
	Memory MemoryInfo     `json:"memory"`
	CPU    CPUInfo        `json:"cpu"`
	Load   [3]float64     `json:"load"`
	Disk   DiskInfo       `json:"disk"`
	DNS    DNSServiceInfo `json:"dns"`
}

// MemoryInfo contains memory usage data.
type MemoryInfo struct {
	RAM MemoryUsage `json:"ram"`
}

// MemoryUsage contains used/total memory values.
type MemoryUsage struct {
	Total float64 `json:"total"`
	Used  float64 `json:"used"`
	Free  float64 `json:"free"`
	Perc  float64 `json:"perc"`
	Unit  string  `json:"unit"`
}

// CPUInfo contains CPU usage data.
type CPUInfo struct {
	Nprocs int     `json:"nprocs"`
	Perc   float64 `json:"perc"`
}

// DiskInfo contains disk usage data.
type DiskInfo struct {
	Total float64 `json:"total"`
	Used  float64 `json:"used"`
	Free  float64 `json:"free"`
	Perc  float64 `json:"perc"`
	Unit  string  `json:"unit"`
}

// DNSServiceInfo contains DNS service state.
type DNSServiceInfo struct {
	Running bool `json:"running"`
}

// HostInfo is the response from GET /api/info/host.
type HostInfo struct {
	Host HostDetails `json:"host"`
	Took float64     `json:"took"`
}

// HostDetails contains host information.
type HostDetails struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	OS     string `json:"os"`
	Kernel string `json:"kernel"`
	Arch   string `json:"arch"`
}

// SensorsInfo is the response from GET /api/info/sensors.
type SensorsInfo struct {
	Sensors SensorsData `json:"sensors"`
	Took    float64     `json:"took"`
}

// SensorsData contains sensor readings.
type SensorsData struct {
	Temperatures []TemperatureReading `json:"list"`
}

// TemperatureReading is a single temperature sensor value.
type TemperatureReading struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Path  string  `json:"path"`
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
	Took              float64 `json:"took"`
}

// MessagesResponse is the response from GET /api/info/messages.
type MessagesResponse struct {
	Messages []FTLMessage `json:"messages"`
	Took     float64      `json:"took"`
}

// FTLMessage is a single FTL diagnostic message.
type FTLMessage struct {
	ID        int    `json:"id"`
	Timestamp int    `json:"timestamp"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	Blob1     string `json:"blob1"`
	Blob2     string `json:"blob2"`
	Blob3     string `json:"blob3"`
	Blob4     string `json:"blob4"`
	Blob5     string `json:"blob5"`
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
type TeleporterImportResponse struct {
	Processed []string `json:"processed"`
	Took      float64  `json:"took"`
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
	ID             int     `json:"id"`
	RemoteAddr     string  `json:"remote_addr"`
	UserAgent      string  `json:"user_agent"`
	ValidUntil     float64 `json:"valid_until"`
	CurrentSession bool    `json:"this"`
}
