package tools

import (
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
)

// twoFakeRegistry starts two fake Pi-holes and returns a registry over them
// plus the fakes for seeding/inspection. "primary" is the default.
func twoFakeRegistry(t *testing.T) (*pihole.Registry, *piholefake.Fake, *piholefake.Fake) {
	t.Helper()
	f1 := piholefake.New()
	t.Cleanup(f1.Close)
	f2 := piholefake.New()
	t.Cleanup(f2.Close)
	reg := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "primary", URL: f1.URL(), Password: "x"},
		{Name: "secondary", URL: f2.URL(), Password: "x"},
	})
	t.Cleanup(reg.Close)
	return reg, f1, f2
}

// seedSource populates a fake with one entry in every synced category.
func seedSource(f *piholefake.Fake) {
	f.AddGroup("kids", "child devices", true)
	f.AddList("block", "https://example.com/ads.txt", "ads", true)
	f.AddDomain("deny", "exact", "tracker.example.com", "tracking", true)
	f.AddClient("192.168.1.50", "laptop")
	f.SetHosts("192.168.1.5 nas.local")
	f.SetCNAMEs("alias.local,nas.local")
}

func syncOutput(t *testing.T, reg *pihole.Registry, args map[string]any) InstanceSyncOutput {
	t.Helper()
	res := callRaw(t, instanceSyncHandler(reg), args)
	if res.IsError {
		t.Fatalf("sync returned error: %s", resultText(res))
	}
	out, ok := res.StructuredContent.(InstanceSyncOutput)
	if !ok {
		t.Fatalf("expected InstanceSyncOutput, got %T", res.StructuredContent)
	}
	return out
}

func TestSync_DiffDetectsDifferences(t *testing.T) {
	reg, f1, _ := twoFakeRegistry(t)
	seedSource(f1)

	res := callRaw(t, instanceDiffHandler(reg), map[string]any{"target": "secondary"})
	if res.IsError {
		t.Fatalf("diff error: %s", resultText(res))
	}
	out, ok := res.StructuredContent.(InstanceDiffOutput)
	if !ok {
		t.Fatalf("expected InstanceDiffOutput, got %T", res.StructuredContent)
	}
	if out.InSync {
		t.Fatal("expected instances to differ")
	}
	// One entry per category: groups, lists, domains, clients, host, cname = 6.
	if out.Added != 6 {
		t.Errorf("expected 6 additions, got %d (categories: %+v)", out.Added, out.Categories)
	}
	if out.Removed != 0 {
		t.Errorf("expected 0 removals, got %d", out.Removed)
	}
}

func TestSync_PlanApplyConverges(t *testing.T) {
	reg, f1, f2 := twoFakeRegistry(t)
	seedSource(f1)

	plan := syncOutput(t, reg, map[string]any{"target": "secondary", "snapshot": false})
	if plan.Mode != "plan" || plan.ConfirmToken == "" {
		t.Fatalf("expected a plan with a token, got %+v", plan)
	}
	if len(plan.Plan) != 6 {
		t.Fatalf("expected 6 planned ops, got %d", len(plan.Plan))
	}

	applied := syncOutput(t, reg, map[string]any{
		"target": "secondary", "mode": "apply", "confirm_token": plan.ConfirmToken, "snapshot": false,
	})
	if applied.Failed != 0 {
		t.Fatalf("expected no failures, got %d (%+v)", applied.Failed, applied.Ops)
	}
	if applied.Applied != 6 {
		t.Errorf("expected 6 applied ops, got %d", applied.Applied)
	}

	// Target now mirrors the source.
	if f2.DomainCount() != 1 || f2.ListCount() != 1 || f2.GroupCount() != 1 {
		t.Errorf("target not reconciled: domains=%d lists=%d groups=%d", f2.DomainCount(), f2.ListCount(), f2.GroupCount())
	}
	if hosts := f2.Hosts(); len(hosts) != 1 || hosts[0] != "192.168.1.5 nas.local" {
		t.Errorf("local DNS not synced: %v", hosts)
	}

	// Re-diff: should now be in sync.
	res := callRaw(t, instanceDiffHandler(reg), map[string]any{"target": "secondary"})
	out := res.StructuredContent.(InstanceDiffOutput)
	if !out.InSync {
		t.Errorf("expected in sync after apply, got added=%d changed=%d", out.Added, out.Changed)
	}
}

func TestSync_Idempotent(t *testing.T) {
	reg, f1, _ := twoFakeRegistry(t)
	seedSource(f1)
	plan := syncOutput(t, reg, map[string]any{"target": "secondary", "snapshot": false})
	syncOutput(t, reg, map[string]any{"target": "secondary", "mode": "apply", "confirm_token": plan.ConfirmToken, "snapshot": false})

	// A second plan should report in-sync with nothing to do.
	plan2 := syncOutput(t, reg, map[string]any{"target": "secondary", "snapshot": false})
	if !plan2.InSync || len(plan2.Plan) != 0 {
		t.Errorf("expected idempotent in-sync plan, got inSync=%v ops=%d", plan2.InSync, len(plan2.Plan))
	}
}

func TestSync_DriftRejectsToken(t *testing.T) {
	reg, f1, _ := twoFakeRegistry(t)
	seedSource(f1)
	plan := syncOutput(t, reg, map[string]any{"target": "secondary", "snapshot": false})

	// Source changes after planning — the old token must be rejected.
	f1.AddDomain("deny", "exact", "newly-added.example.com", "", true)

	res := callRaw(t, instanceSyncHandler(reg), map[string]any{
		"target": "secondary", "mode": "apply", "confirm_token": plan.ConfirmToken, "snapshot": false,
	})
	if !res.IsError {
		t.Fatal("expected drift to reject the stale token")
	}
}

func TestSync_PruneControlsDeletion(t *testing.T) {
	reg, f1, f2 := twoFakeRegistry(t)
	seedSource(f1)
	// Target has an extra domain not on the source.
	f2.AddDomain("deny", "exact", "extra.example.com", "", true)

	// prune=false: never deletes the extra entry.
	plan := syncOutput(t, reg, map[string]any{"target": "secondary", "snapshot": false})
	apply := syncOutput(t, reg, map[string]any{"target": "secondary", "mode": "apply", "confirm_token": plan.ConfirmToken, "snapshot": false})
	for _, op := range apply.Ops {
		if op.Action == "delete" {
			t.Errorf("prune=false must not delete, but planned %+v", op)
		}
	}
	if f2.DomainCount() != 2 { // 1 synced + 1 extra retained
		t.Errorf("expected extra domain retained (count 2), got %d", f2.DomainCount())
	}

	// prune=true: the extra entry is removed.
	pplan := syncOutput(t, reg, map[string]any{"target": "secondary", "prune": true, "snapshot": false})
	syncOutput(t, reg, map[string]any{"target": "secondary", "mode": "apply", "prune": true, "confirm_token": pplan.ConfirmToken, "snapshot": false})
	if f2.DomainCount() != 1 {
		t.Errorf("expected extra domain pruned (count 1), got %d", f2.DomainCount())
	}
}

func TestSync_SelfTargetRejected(t *testing.T) {
	reg, _, _ := twoFakeRegistry(t)
	res := callRaw(t, instanceDiffHandler(reg), map[string]any{"source": "primary", "target": "primary"})
	if !res.IsError {
		t.Fatal("expected self-target to be rejected")
	}
}

func TestSync_UnknownTargetRejected(t *testing.T) {
	reg, _, _ := twoFakeRegistry(t)
	res := callRaw(t, instanceDiffHandler(reg), map[string]any{"target": "ghost"})
	if !res.IsError {
		t.Fatal("expected unknown target to be rejected")
	}
}

func TestSync_CategoryFilter(t *testing.T) {
	reg, f1, _ := twoFakeRegistry(t)
	seedSource(f1)
	res := callRaw(t, instanceDiffHandler(reg), map[string]any{"target": "secondary", "categories": "groups"})
	out := res.StructuredContent.(InstanceDiffOutput)
	if out.Added != 1 {
		t.Errorf("expected only the group addition, got %d", out.Added)
	}
}

func TestSync_ApplyRequiresToken(t *testing.T) {
	reg, f1, _ := twoFakeRegistry(t)
	seedSource(f1)
	res := callRaw(t, instanceSyncHandler(reg), map[string]any{"target": "secondary", "mode": "apply", "snapshot": false})
	if !res.IsError {
		t.Fatal("expected apply without a token to be rejected")
	}
}
