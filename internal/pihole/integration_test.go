//go:build integration

package pihole

import (
	"context"
	"os"
	"testing"
	"time"
)

func integrationClient(t *testing.T) *Client {
	t.Helper()
	url := os.Getenv("PIHOLE_URL")
	if url == "" {
		url = "http://localhost:8081"
	}
	password := os.Getenv("PIHOLE_PASSWORD")
	if password == "" {
		password = "test"
	}
	c := New(url, password, WithTimeout(10*time.Second))
	// Log out when the test ends. Each leaked session holds one of Pi-hole's
	// API seats (16 by default) for half an hour, and a run of these tests
	// against a shared Pi-hole used to use most of them up.
	t.Cleanup(c.Close)
	return c
}

func TestIntegration_Auth(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// First call triggers lazy auth.
	var status BlockingStatus
	if err := c.Get(ctx, "/dns/blocking", &status); err != nil {
		t.Fatalf("auth + get blocking failed: %v", err)
	}
	if status.Blocking != "enabled" && status.Blocking != "disabled" {
		t.Errorf("unexpected blocking status: %s", status.Blocking)
	}
}

func TestIntegration_DNSBlocking(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// Get current state.
	var before BlockingStatus
	if err := c.Get(ctx, "/dns/blocking", &before); err != nil {
		t.Fatalf("get blocking: %v", err)
	}

	// Toggle with a short timer so it reverts.
	toggle := !stringToBool(before.Blocking)
	timer := 3.0
	body := BlockingRequest{Blocking: toggle, Timer: &timer}
	var after BlockingStatus
	if err := c.Post(ctx, "/dns/blocking", body, &after); err != nil {
		t.Fatalf("set blocking: %v", err)
	}
	if after.Timer == nil {
		t.Error("expected timer to be set")
	}
}

func TestIntegration_DomainsCRUD(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	domain := "integration-test-" + time.Now().Format("150405") + ".example.com"

	// Add.
	addBody := map[string]any{"domain": domain, "comment": "integration test"}
	var addResult DomainsResponse
	if err := c.Post(ctx, "/domains/deny/exact", addBody, &addResult); err != nil {
		t.Fatalf("add domain: %v", err)
	}

	// List — should contain our domain.
	var listResult DomainsResponse
	if err := c.Get(ctx, "/domains/deny/exact", &listResult); err != nil {
		t.Fatalf("list domains: %v", err)
	}
	found := false
	for _, d := range listResult.Domains {
		if d.Domain == domain {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("added domain %s not found in list", domain)
	}

	// Delete.
	if err := c.Delete(ctx, "/domains/deny/exact/"+domain); err != nil {
		t.Fatalf("delete domain: %v", err)
	}
}

func TestIntegration_GroupsCRUD(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	name := "inttest-" + time.Now().Format("150405")

	// Add.
	if err := c.Post(ctx, "/groups", map[string]any{"name": name, "comment": "test"}, nil); err != nil {
		t.Fatalf("add group: %v", err)
	}

	// List.
	var result GroupsResponse
	if err := c.Get(ctx, "/groups", &result); err != nil {
		t.Fatalf("list groups: %v", err)
	}
	found := false
	for _, g := range result.Groups {
		if g.Name == name {
			found = true
		}
	}
	if !found {
		t.Errorf("added group %s not found", name)
	}

	// Delete.
	if err := c.Delete(ctx, "/groups/"+name); err != nil {
		t.Fatalf("delete group: %v", err)
	}
}

func TestIntegration_StatsSummary(t *testing.T) {
	c := integrationClient(t)
	var stats StatsSummary
	if err := c.Get(context.Background(), "/stats/summary", &stats); err != nil {
		t.Fatalf("stats summary: %v", err)
	}
	if stats.Gravity.DomainsBeingBlocked <= 0 {
		t.Error("expected gravity domains > 0")
	}
}

func TestIntegration_Logs(t *testing.T) {
	c := integrationClient(t)
	var logs LogResponse
	if err := c.Get(context.Background(), "/logs/dnsmasq", &logs); err != nil {
		t.Fatalf("get logs: %v", err)
	}
	if len(logs.Log) == 0 {
		t.Skip("no log entries (fresh instance)")
	}
	// Validate LogEntry struct fields.
	if logs.Log[0].Message == "" {
		t.Error("expected non-empty log message")
	}
}

func TestIntegration_InfoVersion(t *testing.T) {
	c := integrationClient(t)
	var ver VersionInfo
	if err := c.Get(context.Background(), "/info/version", &ver); err != nil {
		t.Fatalf("info version: %v", err)
	}
	if ver.Version.FTL.Local.Version == "" {
		t.Error("expected non-empty FTL version")
	}
}

func stringToBool(s string) bool {
	return s == "enabled"
}
