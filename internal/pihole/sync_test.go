package pihole_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
)

// newSyncPair returns a source and target fake with clients wired to them.
func newSyncPair(t *testing.T) (srcFake, tgtFake *piholefake.Fake, src, tgt *pihole.Client) {
	t.Helper()
	srcFake = piholefake.New()
	t.Cleanup(srcFake.Close)
	tgtFake = piholefake.New()
	t.Cleanup(tgtFake.Close)
	src = pihole.New(srcFake.URL(), "test", pihole.WithName("primary"), pihole.WithRetry(0, time.Second))
	tgt = pihole.New(tgtFake.URL(), "test", pihole.WithName("secondary"), pihole.WithRetry(0, time.Second))
	return srcFake, tgtFake, src, tgt
}

// seedSource populates the source fake with one entry per category.
func seedSource(f *piholefake.Fake) {
	f.AddGroup("iot", "smart devices", true)
	f.AddList("block", "https://lists.example.com/ads.txt", "ad list", true)
	f.AddDomain("deny", "exact", "ads.example.com", "blocked", true)
	f.AddClient("192.168.1.50", "laptop")
	f.SetHosts("192.168.1.2 pi.hole")
	f.SetCNAMEs("alias.lan,target.lan")
}

func TestComputeDiff_EmptyInstancesInSync(t *testing.T) {
	_, _, src, tgt := newSyncPair(t)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	if len(d.Categories) != len(pihole.DefaultSyncCategories) {
		t.Errorf("categories = %d, want %d", len(d.Categories), len(pihole.DefaultSyncCategories))
	}
	added, changed, removed := d.Totals()
	if added+changed+removed != 0 {
		t.Errorf("Totals() = %d/%d/%d, want all zero", added, changed, removed)
	}
	if !d.InSync(false) || !d.InSync(true) {
		t.Error("empty instances should be in sync with and without prune")
	}
	if d.Source != "primary" || d.Target != "secondary" {
		t.Errorf("Source/Target = %q/%q", d.Source, d.Target)
	}
}

func TestComputeDiff_AddedChangedRemoved(t *testing.T) {
	srcFake, tgtFake, src, tgt := newSyncPair(t)
	seedSource(srcFake)
	// Same domain key on the target with a different comment → changed.
	tgtFake.AddDomain("deny", "exact", "ads.example.com", "different comment", true)
	// Target-only group → removed.
	tgtFake.AddGroup("stale", "no longer on source", true)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	added, changed, removed := d.Totals()
	// Added: group iot, list, client, hosts entry, cname entry = 5.
	// Changed: the domain comment. Removed: the stale group.
	if added != 5 || changed != 1 || removed != 1 {
		t.Errorf("Totals() = %d/%d/%d, want 5/1/1", added, changed, removed)
	}
	if d.InSync(false) || d.InSync(true) {
		t.Error("differing instances must not report in sync")
	}

	byCat := map[string]pihole.CategoryCount{}
	for _, c := range d.Counts() {
		byCat[c.Category] = c
	}
	if c := byCat["groups"]; c.Added != 1 || c.Removed != 1 {
		t.Errorf("groups count = %+v, want 1 added, 1 removed", c)
	}
	if c := byCat["domains"]; c.Changed != 1 {
		t.Errorf("domains count = %+v, want 1 changed", c)
	}
	if c := byCat["local_dns"]; c.Added != 1 {
		t.Errorf("local_dns count = %+v, want 1 added", c)
	}
	if byCat["groups"].Label != "groups" || byCat["cname"].Label != "CNAME records" {
		t.Errorf("labels wrong: %+v", byCat)
	}
}

func TestDiff_PlanOrderingAndPrune(t *testing.T) {
	srcFake, tgtFake, src, tgt := newSyncPair(t)
	seedSource(srcFake)
	tgtFake.AddGroup("stale", "", true)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	plan := d.Plan(false)
	if len(plan) != 6 {
		t.Fatalf("plan steps = %d, want 6 adds", len(plan))
	}
	// Adds run in applyOrder: groups must precede domains.
	if plan[0].Category != "groups" || plan[0].Action != "add" {
		t.Errorf("first step = %+v, want groups add", plan[0])
	}
	for _, s := range plan {
		if s.Action == "delete" {
			t.Errorf("plan without prune contains delete: %+v", s)
		}
	}

	pruned := d.Plan(true)
	if len(pruned) != 7 {
		t.Fatalf("pruned plan steps = %d, want 7", len(pruned))
	}
	last := pruned[len(pruned)-1]
	// Deletes run in reverse category order; the only delete is the stale
	// group, and groups are first in applyOrder, so it must come last.
	if last.Action != "delete" || last.Category != "groups" {
		t.Errorf("last pruned step = %+v, want groups delete", last)
	}
}

func TestDiff_TokenDeterministicAndDriftSensitive(t *testing.T) {
	srcFake, tgtFake, src, tgt := newSyncPair(t)
	seedSource(srcFake)
	tgtFake.AddGroup("stale", "", true)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	first, second := d.Token(false), d.Token(false)
	if first != second {
		t.Errorf("Token is not deterministic: %q vs %q", first, second)
	}
	if d.Token(false) == d.Token(true) {
		t.Error("prune must change the token when removals exist")
	}

	// Drift: an extra source entry changes the plan, so the token changes.
	before := d.Token(false)
	srcFake.AddDomain("allow", "exact", "safe.example.com", "", true)
	d2, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff after drift: %v", err)
	}
	if d2.Token(false) == before {
		t.Error("token unchanged after configuration drift")
	}
}

func TestApplyPlan_ReconcilesTarget(t *testing.T) {
	srcFake, tgtFake, src, tgt := newSyncPair(t)
	seedSource(srcFake)
	tgtFake.AddDomain("deny", "exact", "ads.example.com", "different comment", true)
	tgtFake.AddGroup("stale", "", true)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	res := pihole.ApplyPlan(context.Background(), tgt, d, true)
	if res.Failed != 0 {
		t.Fatalf("ApplyPlan failed ops: %d (%+v)", res.Failed, res.Ops)
	}
	if res.Applied != len(d.Plan(true)) {
		t.Errorf("Applied = %d, want %d", res.Applied, len(d.Plan(true)))
	}

	// The target now matches the source.
	d2, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff after apply: %v", err)
	}
	if !d2.InSync(true) {
		a, c, r := d2.Totals()
		t.Errorf("target not in sync after apply: %d/%d/%d", a, c, r)
	}
	if got := tgtFake.GroupCount(); got != 1 {
		t.Errorf("target groups = %d, want 1 (stale pruned, iot added)", got)
	}
	if got := tgtFake.Hosts(); len(got) != 1 || got[0] != "192.168.1.2 pi.hole" {
		t.Errorf("target hosts = %v", got)
	}
}

func TestApplyPlan_ContinuesPastFailures(t *testing.T) {
	srcFake, tgtFake, src, tgt := newSyncPair(t)
	seedSource(srcFake)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	// Kill the target: every operation should fail, none should abort the run.
	tgtFake.Close()
	res := pihole.ApplyPlan(context.Background(), tgt, d, false)
	if res.Applied != 0 {
		t.Errorf("Applied = %d, want 0 against a dead target", res.Applied)
	}
	if res.Failed != len(res.Ops) || res.Failed == 0 {
		t.Errorf("Failed = %d of %d ops, want all", res.Failed, len(res.Ops))
	}
	for _, op := range res.Ops {
		if op.Err == "" {
			t.Errorf("op %+v missing error", op)
		}
	}
}

func TestApplyPlan_CancelledContext(t *testing.T) {
	srcFake, _, src, tgt := newSyncPair(t)
	seedSource(srcFake)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := pihole.ApplyPlan(ctx, tgt, d, false)
	if res.Applied != 0 {
		t.Errorf("Applied = %d, want 0 with cancelled context", res.Applied)
	}
	if res.Failed != len(res.Ops) {
		t.Errorf("Failed = %d, want %d", res.Failed, len(res.Ops))
	}
	for _, op := range res.Ops {
		if !strings.Contains(op.Err, "context canceled") {
			t.Errorf("op error = %q, want context cancellation", op.Err)
		}
	}
}

func TestParseSyncCategory(t *testing.T) {
	for _, c := range pihole.DefaultSyncCategories {
		got, err := pihole.ParseSyncCategory(string(c))
		if err != nil || got != c {
			t.Errorf("pihole.ParseSyncCategory(%q) = %q, %v", c, got, err)
		}
	}
	if _, err := pihole.ParseSyncCategory(" groups "); err != nil {
		t.Errorf("whitespace should be trimmed: %v", err)
	}
	_, err := pihole.ParseSyncCategory("bogus")
	if err == nil {
		t.Fatal("expected error for unknown category")
	}
	if !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "valid categories") {
		t.Errorf("error %q should name the input and list valid categories", err)
	}
}
