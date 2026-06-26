//go:build integration

package pihole

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// multiRegistry builds a two-instance registry from PIHOLE_1_*/PIHOLE_2_*,
// falling back to the CI compose defaults (8081 + 8082). Tests that need a
// second instance skip when only one is configured.
func multiRegistry(t *testing.T) *Registry {
	t.Helper()
	url1 := envOr("PIHOLE_1_URL", "http://localhost:8081")
	url2 := envOr("PIHOLE_2_URL", "")
	if url2 == "" {
		t.Skip("PIHOLE_2_URL not set; skipping multi-instance integration test")
	}
	return NewRegistry([]InstanceConfig{
		{Name: "primary", URL: url1, Password: envOr("PIHOLE_1_PASSWORD", "test")},
		{Name: "secondary", URL: url2, Password: envOr("PIHOLE_2_PASSWORD", "test")},
	}, WithTimeout(15*time.Second))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func TestIntegrationMulti_Routing(t *testing.T) {
	reg := multiRegistry(t)
	defer reg.Close()

	if got := reg.Names(); len(got) != 2 || got[0] != "primary" {
		t.Fatalf("unexpected instance names: %v", got)
	}
	if reg.Default().Name() != "primary" {
		t.Errorf("default should be primary, got %s", reg.Default().Name())
	}
	for _, name := range reg.Names() {
		c, err := reg.Get(name)
		if err != nil {
			t.Fatalf("Get(%s): %v", name, err)
		}
		var s StatsSummary
		if err := c.Get(context.Background(), "/stats/summary", &s); err != nil {
			t.Errorf("instance %s unreachable: %v", name, err)
		}
	}
}

// TestIntegrationMulti_DiffSyncRoundTrip adds a unique allow rule on the
// primary, syncs the domains category to the secondary, and verifies the two
// converge. It cleans up the rule on both instances afterwards.
func TestIntegrationMulti_DiffSyncRoundTrip(t *testing.T) {
	reg := multiRegistry(t)
	defer reg.Close()
	ctx := context.Background()

	source := reg.Default()
	target, err := reg.Get("secondary")
	if err != nil {
		t.Fatal(err)
	}

	const marker = "sync-roundtrip-test.example.com"
	addPath := "/domains/allow/exact"
	singlePath := addPath + "/" + marker
	defer func() {
		_ = source.Delete(ctx, singlePath)
		_ = target.Delete(ctx, singlePath)
	}()

	if err := source.Post(ctx, addPath, map[string]any{"domain": marker, "comment": "roundtrip", "enabled": true}, nil); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	diff, err := ComputeDiff(ctx, source, target, []SyncCategory{CategoryDomains})
	if err != nil {
		t.Fatalf("compute diff: %v", err)
	}
	if !containsMarker(diff.Plan(false), marker) {
		t.Fatalf("expected the seeded domain in the plan, got %+v", diff.Plan(false))
	}

	res := ApplyPlan(ctx, target, diff, false)
	if res.Failed != 0 {
		t.Fatalf("apply had failures: %+v", res.Ops)
	}

	after, err := ComputeDiff(ctx, source, target, []SyncCategory{CategoryDomains})
	if err != nil {
		t.Fatalf("re-diff: %v", err)
	}
	if !after.InSync(false) {
		t.Errorf("expected domains in sync after apply; plan still: %+v", after.Plan(false))
	}
}

func containsMarker(steps []PlanStep, marker string) bool {
	for _, s := range steps {
		if strings.Contains(s.Item, marker) {
			return true
		}
	}
	return false
}
