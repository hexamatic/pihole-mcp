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
