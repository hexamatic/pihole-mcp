package pihole

// PADDResponse is the response from GET /api/padd — a single-call dashboard
// snapshot that combines query statistics, blocking state, top items, cache
// counters, the primary network interface, hardware sensors, component
// versions, and host identity. It mirrors the data the PADD terminal display
// consumes, so one call replaces roughly six individual stats/info/network
// requests.
//
// The `config` block is decoded leniently into a map because the Pi-hole API
// returns mixed value types there; the typed fields below cover everything the
// dashboard tool surfaces.
type PADDResponse struct {
	Blocking      string  `json:"blocking"`
	ActiveClients int     `json:"active_clients"`
	GravitySize   int     `json:"gravity_size"`
	RecentBlocked *string `json:"recent_blocked"`
	TopDomain     *string `json:"top_domain"`
	TopBlocked    *string `json:"top_blocked"`
	TopClient     *string `json:"top_client"`
	NodeName      string  `json:"node_name"`
	HostModel     *string `json:"host_model"`
	PID           int     `json:"pid"`
	CPUPercent    float64 `json:"%cpu"`
	MemPercent    float64 `json:"%mem"`

	Queries PADDQueries    `json:"queries"`
	Cache   PADDCache      `json:"cache"`
	Iface   PADDIface      `json:"iface"`
	Sensors PADDSensors    `json:"sensors"`
	Version VersionDetails `json:"version"`
	Config  map[string]any `json:"config,omitempty"`

	Took float64 `json:"took"`
}

// PADDQueries holds the headline query counters, including the query_frequency
// value introduced in Pi-hole FTL v6.6.
type PADDQueries struct {
	Total          int     `json:"total"`
	Blocked        int     `json:"blocked"`
	PercentBlocked float64 `json:"percent_blocked"`
	QueryFrequency float64 `json:"query_frequency"`
}

// PADDCache holds DNS cache utilisation counters.
type PADDCache struct {
	Size     int `json:"size"`
	Inserted int `json:"inserted"`
	Evicted  int `json:"evicted"`
}

// PADDIface holds the primary IPv4 and IPv6 interface summaries.
type PADDIface struct {
	V4 *PADDInterface `json:"v4,omitempty"`
	V6 *PADDInterface `json:"v6,omitempty"`
}

// PADDInterface describes a single network interface as reported by /padd.
// RxBytes/TxBytes reuse the {unit, value} envelope shared with
// network_interfaces (see NetworkInterfaceValue).
type PADDInterface struct {
	Addr     *string                `json:"addr"`
	Name     string                 `json:"name"`
	GWAddr   *string                `json:"gw_addr"`
	NumAddrs int                    `json:"num_addrs"`
	RxBytes  *NetworkInterfaceValue `json:"rx_bytes,omitempty"`
	TxBytes  *NetworkInterfaceValue `json:"tx_bytes,omitempty"`
}

// PADDSensors is the simplified sensor summary /padd returns — a single CPU
// temperature plus its hot limit. This differs from the richer multi-sensor
// shape returned by /info/sensors (SensorsInfo).
type PADDSensors struct {
	CPUTemp  *float64 `json:"cpu_temp"`
	HotLimit float64  `json:"hot_limit"`
	Unit     string   `json:"unit"`
}
