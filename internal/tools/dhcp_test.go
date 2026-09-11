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
	rec := piholeHandler(map[string]any{
		"/dhcp/leases/192.168.1.10": nil,
	})
	c := newTestClient(t, rec)

	text := callTool(t, dhcpDeleteLeaseHandler, c, map[string]any{
		"ip": "192.168.1.10",
	})
	if !strings.Contains(text, "Deleted") {
		t.Errorf("expected 'Deleted' message, got: %s", text)
	}
	if !strings.Contains(text, "192.168.1.10") {
		t.Errorf("expected IP in response, got: %s", text)
	}

	// Both assertions above are built from the tool's own ip argument, so they
	// stay green whatever went onto the wire, including a POST that deleted
	// nothing. The address goes into the path, so pin the path as well: a
	// DELETE of /dhcp/leases with no ip is the whole lease table.
	req := rec.Only(t, "DELETE", "/dhcp/leases/192.168.1.10")
	req.AssertRawPath(t, "/dhcp/leases/192.168.1.10")
	req.AssertNoBody(t)
	req.AssertNoQueryString(t)
}

// A missing ip must fail before anything is sent. Deleting the whole lease
// table because an argument was absent is the worst outcome this tool has.
func TestDHCPDeleteLease_MissingIPNeverReachesTheAPI(t *testing.T) {
	rec := piholeHandler(map[string]any{})
	c := newTestClient(t, rec)

	callToolExpectError(t, dhcpDeleteLeaseHandler, c, nil)

	rec.AssertNone(t, "", "")
}
