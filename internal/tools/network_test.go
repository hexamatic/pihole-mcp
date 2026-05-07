package tools

import (
	"strings"
	"testing"
)

func TestNetworkDevices_Normal(t *testing.T) {
	vendor := "Apple"
	hostname := "desktop"

	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/devices": map[string]any{
			"devices": []any{
				map[string]any{
					"id": 1, "hwaddr": "AA:BB:CC:DD:EE:FF", "interface": "eth0",
					"firstSeen": 1700000000, "lastQuery": 1700000000, "numQueries": 1500,
					"macVendor": vendor,
					"ips": []any{
						map[string]any{"ip": "192.168.1.10", "name": hostname, "lastSeen": 1700000000, "nameUpdated": 1700000000},
					},
				},
			},
		},
	}))

	text := callTool(t, networkDevicesHandler, c, nil)
	if !strings.Contains(text, "1 devices") {
		t.Errorf("expected device count, got: %s", text)
	}
	if !strings.Contains(text, "AA:BB:CC:DD:EE:FF") {
		t.Errorf("expected MAC address, got: %s", text)
	}
	if !strings.Contains(text, "Apple") {
		t.Errorf("expected vendor, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected IP address, got: %s", text)
	}
	if !strings.Contains(text, "1,500") {
		t.Errorf("expected formatted query count, got: %s", text)
	}
}

func TestNetworkDevices_Minimal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/devices": map[string]any{
			"devices": []any{
				map[string]any{"id": 1, "hwaddr": "AA:BB:CC:DD:EE:FF", "interface": "eth0", "firstSeen": 1700000000, "lastQuery": 1700000000, "numQueries": 100, "ips": []any{}},
			},
		},
	}))

	text := callTool(t, networkDevicesHandler, c, map[string]any{"detail": "minimal"})
	if !strings.Contains(text, "1 network devices.") {
		t.Errorf("expected minimal count, got: %s", text)
	}
}

func TestNetworkDevices_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/devices": map[string]any{
			"devices": []any{
				map[string]any{"id": 1, "hwaddr": "AA:BB:CC:DD:EE:FF", "interface": "eth0", "firstSeen": 1700000000, "lastQuery": 1700000000, "numQueries": 100, "ips": []any{map[string]any{"ip": "192.168.1.10", "lastSeen": 1700000000, "nameUpdated": 1700000000}}},
			},
		},
	}))

	text := callTool(t, networkDevicesHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "ID,MAC,Vendor,IPs,Queries,LastQuery") {
		t.Errorf("expected CSV headers, got: %s", text)
	}
	if !strings.Contains(text, "AA:BB:CC:DD:EE:FF") {
		t.Errorf("expected MAC in CSV, got: %s", text)
	}
}

func TestNetworkDevices_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/devices": map[string]any{"devices": []any{}},
	}))

	text := callTool(t, networkDevicesHandler, c, nil)
	if text != "No network devices found." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestNetworkDevices_NilVendor(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/devices": map[string]any{
			"devices": []any{
				map[string]any{
					"id": 1, "hwaddr": "AA:BB:CC:DD:EE:FF", "interface": "eth0",
					"firstSeen": 1700000000, "lastQuery": 1700000000, "numQueries": 50,
					"macVendor": nil,
					"ips":       []any{},
				},
			},
		},
	}))

	text := callTool(t, networkDevicesHandler, c, nil)
	if !strings.Contains(text, "unknown") {
		t.Errorf("expected 'unknown' for nil vendor, got: %s", text)
	}
}

func TestNetworkGateway_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/gateway": map[string]any{
			"gateway": []any{
				map[string]any{"address": "192.168.1.1", "family": "inet", "interface": "eth0", "local": []any{"192.168.1.100"}},
			},
		},
	}))

	text := callTool(t, networkGatewayHandler, c, nil)
	if !strings.Contains(text, "Gateway") {
		t.Errorf("expected gateway header, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.1") {
		t.Errorf("expected gateway address, got: %s", text)
	}
	if !strings.Contains(text, "inet") {
		t.Errorf("expected address family, got: %s", text)
	}
}

func TestNetworkGateway_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/gateway": map[string]any{"gateway": []any{}},
	}))

	text := callTool(t, networkGatewayHandler, c, nil)
	if text != "No gateway information available." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestNetworkInfo_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/routes":     map[string]any{"routes": []any{map[string]any{"family": "inet", "dst": "default", "gateway": "192.168.1.1", "oif": "eth0"}}},
		"/network/interfaces": map[string]any{"interfaces": []any{map[string]any{"name": "eth0", "type": "ether", "state": "UP"}}},
	}))

	text := callTool(t, networkInfoHandler, c, nil)
	if !strings.Contains(text, "1 routes") {
		t.Errorf("expected route count, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.1") {
		t.Errorf("expected gateway in routes, got: %s", text)
	}
	if !strings.Contains(text, "1 interfaces") {
		t.Errorf("expected interface count, got: %s", text)
	}
	if !strings.Contains(text, "eth0") {
		t.Errorf("expected interface name, got: %s", text)
	}
	if !strings.Contains(text, "UP") {
		t.Errorf("expected interface state, got: %s", text)
	}
}

func TestNetworkRoutes_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/routes": map[string]any{
			"routes": []any{
				map[string]any{"family": "inet", "dst": "default", "gateway": "192.168.1.1", "dev": "eth0", "scope": "global", "src": "192.168.1.100"},
				map[string]any{"family": "inet", "dst": "192.168.1.0/24", "dev": "eth0", "scope": "link"},
			},
		},
	}))

	text := callTool(t, networkRoutesHandler, c, nil)
	if !strings.Contains(text, "2 routes") {
		t.Errorf("expected route count, got: %s", text)
	}
	if !strings.Contains(text, "default via 192.168.1.1 on eth0") {
		t.Errorf("expected formatted default route, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.0/24 via direct on eth0") {
		t.Errorf("expected formatted link route with 'direct' fallback, got: %s", text)
	}
	if !strings.Contains(text, "scope global") {
		t.Errorf("expected scope label, got: %s", text)
	}
	if !strings.Contains(text, "src 192.168.1.100") {
		t.Errorf("expected source label, got: %s", text)
	}
}

func TestNetworkRoutes_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/routes": map[string]any{"routes": []any{}},
	}))

	text := callTool(t, networkRoutesHandler, c, nil)
	if text != "No routes available." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestNetworkInterfaces_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/interfaces": map[string]any{
			"interfaces": []any{
				map[string]any{
					"name": "eth0", "type": "ether", "state": "UP", "speed": 1000,
					"carrier": true,
					"addresses": []any{
						map[string]any{"family": "inet", "address": "192.168.1.100", "prefixlen": 24, "scope": "global", "local": "192.168.1.100"},
						map[string]any{"family": "inet6", "address": "fe80::1", "prefixlen": 64, "scope": "link"},
					},
					"stats": map[string]any{
						"bits":     64,
						"rx_bytes": map[string]any{"unit": "M", "value": 12.5},
						"tx_bytes": map[string]any{"unit": "M", "value": 8.0},
					},
				},
			},
		},
	}))

	text := callTool(t, networkInterfacesHandler, c, nil)
	if !strings.Contains(text, "1 interfaces") {
		t.Errorf("expected interface count, got: %s", text)
	}
	if !strings.Contains(text, "**eth0**") {
		t.Errorf("expected formatted interface name, got: %s", text)
	}
	if !strings.Contains(text, "speed=1000Mb/s") {
		t.Errorf("expected speed label, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.100/24") {
		t.Errorf("expected IPv4 with prefix, got: %s", text)
	}
	if !strings.Contains(text, "fe80::1/64") {
		t.Errorf("expected IPv6 with prefix, got: %s", text)
	}
	if !strings.Contains(text, "rx 12.50MB") {
		t.Errorf("expected rx stats with unit, got: %s", text)
	}
	if !strings.Contains(text, "tx 8.00MB") {
		t.Errorf("expected tx stats with unit, got: %s", text)
	}
}

func TestNetworkInterfaces_NullSpeed(t *testing.T) {
	// Loopback interfaces have speed=null in the real API; the handler must
	// not crash and must omit the speed label.
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/interfaces": map[string]any{
			"interfaces": []any{
				map[string]any{
					"name":  "lo",
					"type":  "loopback",
					"state": "unknown",
					"speed": nil,
				},
			},
		},
	}))

	text := callTool(t, networkInterfacesHandler, c, nil)
	if !strings.Contains(text, "**lo**") {
		t.Errorf("expected loopback interface name, got: %s", text)
	}
	if strings.Contains(text, "speed=") {
		t.Errorf("speed label should be absent for null speed, got: %s", text)
	}
}

func TestNetworkInterfaces_RealFixture(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/interfaces": loadFixture(t, "network_interfaces"),
	}))

	text := callTool(t, networkInterfacesHandler, c, nil)
	if text == "" {
		t.Fatal("expected non-empty output from real fixture")
	}
	if !strings.Contains(text, "interfaces:") {
		t.Errorf("expected interfaces header, got: %s", text)
	}
}

func TestNetworkInterfaces_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/interfaces": map[string]any{"interfaces": []any{}},
	}))

	text := callTool(t, networkInterfacesHandler, c, nil)
	if text != "No interfaces available." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestNetworkDeleteDevice_Success(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/network/devices/42": nil,
	}))

	text := callTool(t, networkDeleteDeviceHandler, c, map[string]any{"id": 42.0})
	if !strings.Contains(text, "Device 42 deleted") {
		t.Errorf("expected deletion confirmation, got: %s", text)
	}
}

func TestNetworkDeleteDevice_MissingID(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{}))

	text := callToolExpectError(t, networkDeleteDeviceHandler, c, nil)
	if !strings.Contains(text, "Parameter 'id' is required") {
		t.Errorf("expected required-id error, got: %s", text)
	}
}

func TestNetworkDeleteDevice_ZeroID(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{}))

	text := callToolExpectError(t, networkDeleteDeviceHandler, c, map[string]any{"id": 0.0})
	if !strings.Contains(text, "must be a positive integer") {
		t.Errorf("expected positive-integer error, got: %s", text)
	}
}
