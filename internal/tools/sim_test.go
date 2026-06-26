//go:build sim

// Package tools sim harness: a Docker-free, in-process walkthrough of the full
// multi-instance flow (routing, instance=all aggregation, diff, and a
// plan→apply→converge sync) against fake Pi-holes. Run with `just sim`.
//
// It is gated behind the `sim` build tag so it does not run as part of the
// normal unit suite; it exists to make the multi-instance behaviour observable
// locally without standing up containers.
package tools

import (
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestSimulation(t *testing.T) {
	f1 := piholefake.New()
	defer f1.Close()
	f2 := piholefake.New()
	defer f2.Close()

	f1.SetSummaryTotal(1200)
	f2.SetSummaryTotal(800)
	seedSource(f1)

	reg := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "downstairs", URL: f1.URL(), Password: "x"},
		{Name: "upstairs", URL: f2.URL(), Password: "x"},
	})
	defer reg.Close()

	t.Log("== Two fake Pi-holes: downstairs (default) and upstairs ==")

	t.Log("\n-- stats_summary, instance=all (parallel aggregate) --")
	tool := mcp.NewTool("pihole_stats_summary", mcp.WithReadOnlyHintAnnotation(true))
	agg := instanceAware(reg, tool, statsSummaryHandler(reg))
	t.Log(resultText(callRaw(t, agg, map[string]any{"instance": "all"})))

	t.Log("\n-- instance_diff: downstairs -> upstairs --")
	t.Log(resultText(callRaw(t, instanceDiffHandler(reg), map[string]any{"target": "upstairs"})))

	t.Log("\n-- instance_sync plan --")
	plan := syncOutput(t, reg, map[string]any{"target": "upstairs", "snapshot": false})
	t.Logf("plan: %d ops, confirm_token=%s", len(plan.Plan), plan.ConfirmToken)

	t.Log("\n-- instance_sync apply --")
	applied := syncOutput(t, reg, map[string]any{
		"target": "upstairs", "mode": "apply", "confirm_token": plan.ConfirmToken, "snapshot": false,
	})
	t.Logf("applied=%d failed=%d", applied.Applied, applied.Failed)
	if applied.Failed != 0 {
		t.Fatalf("simulation sync had failures: %+v", applied.Ops)
	}

	t.Log("\n-- re-diff: should now be in sync --")
	res := callRaw(t, instanceDiffHandler(reg), map[string]any{"target": "upstairs"})
	out := res.StructuredContent.(InstanceDiffOutput)
	if !out.InSync {
		t.Fatalf("expected in sync after apply, got added=%d changed=%d", out.Added, out.Changed)
	}
	t.Log(resultText(res))
	t.Log("\n== Simulation complete: instances converged ==")
}
