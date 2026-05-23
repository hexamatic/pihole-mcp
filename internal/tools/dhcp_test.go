package tools

import (
	"strings"
	"testing"
)

func TestDHCPLeases_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/dhcp/leases": map[string]any{
			"leases": []any{
				map[string]any{"expires": 1700003600, "name": "desktop", "hwaddr": "AA:BB:CC:DD:EE:FF", "ip": "192.168.1.10", "clientid": "01:aa:bb:cc:dd:ee:ff"},
				map[string]any{"expires": 1700007200, "name": "laptop", "hwaddr": "11:22:33:44:55:66", "ip": "192.168.1.20", "clientid": ""},
			},
		},
	}))

	text := callTool(t, dhcpLeasesHandler, c, nil)
	if !strings.Contains(text, "2 leases") {
		t.Errorf("expected '2 leases' header, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected first lease IP, got: %s", text)
	}
	if !strings.Contains(text, "desktop") {
		t.Errorf("expected first lease name, got: %s", text)
	}
	if !strings.Contains(text, "AA:BB:CC:DD:EE:FF") {
		t.Errorf("expected first lease MAC, got: %s", text)
	}
}

func TestDHCPLeases_NeverExpires(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/dhcp/leases": map[string]any{
			"leases": []any{
				map[string]any{"expires": 0, "name": "server", "hwaddr": "11:22:33:44:55:66", "ip": "192.168.1.20", "clientid": ""},
			},
		},
	}))

	text := callTool(t, dhcpLeasesHandler, c, nil)
	if !strings.Contains(text, "never") {
		t.Errorf("expected 'never' for zero expiry, got: %s", text)
	}
}

func TestDHCPLeases_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/dhcp/leases": map[string]any{"leases": []any{}},
	}))

	text := callTool(t, dhcpLeasesHandler, c, nil)
	if !strings.Contains(text, "No active DHCP leases") {
		t.Errorf("expected empty leases message, got: %s", text)
	}
}

func TestDHCPLeases_CSV(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/dhcp/leases": map[string]any{
			"leases": []any{
				map[string]any{
					"ip":      "192.168.1.42",
					"name":    "kid-tablet",
					"hwaddr":  "AA:BB:CC:DD:EE:FF",
					"expires": 1777608098,
				},
			},
		},
	}))

	text := callTool(t, dhcpLeasesHandler, c, map[string]any{"format": "csv"})
	if !strings.Contains(text, "IP,Hostname,MAC,Expires") {
		t.Errorf("CSV should have header row, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.42,kid-tablet,AA:BB:CC:DD:EE:FF,") {
		t.Errorf("CSV should contain lease row, got: %s", text)
	}
}

func TestDHCPDeleteLease_Success(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/dhcp/leases/192.168.1.10": nil,
	}))

	text := callTool(t, dhcpDeleteLeaseHandler, c, map[string]any{
		"ip": "192.168.1.10",
	})
	if !strings.Contains(text, "Deleted") {
		t.Errorf("expected 'Deleted' message, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected IP in response, got: %s", text)
	}
}
