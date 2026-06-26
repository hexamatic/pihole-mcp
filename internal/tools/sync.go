package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// syncCategoriesDesc lists the categories accepted by the diff/sync tools.
const syncCategoriesDesc = "Comma-separated categories to compare. Default: all of groups, lists, domains, clients, local_dns, cname. Host-specific and secret config (DHCP, interfaces, passwords, TLS) is never synced."

// InstanceDiffOutput is the structured output for pihole_instance_diff.
type InstanceDiffOutput struct {
	Source     string               `json:"source" jsonschema:"Instance treated as the source of truth"`
	Target     string               `json:"target" jsonschema:"Instance compared against the source"`
	InSync     bool                 `json:"in_sync" jsonschema:"True when the target already matches the source"`
	Added      int                  `json:"added" jsonschema:"Entries on the source but missing from the target"`
	Changed    int                  `json:"changed" jsonschema:"Entries present on both but differing"`
	Removed    int                  `json:"removed" jsonschema:"Entries on the target but missing from the source"`
	Categories []DiffCategoryOutput `json:"categories" jsonschema:"Per-category difference counts"`
	Plan       []pihole.PlanStep    `json:"plan,omitempty" jsonschema:"Ordered add/update steps a non-pruning sync would apply"`
}

// DiffCategoryOutput is the per-category breakdown for a diff.
type DiffCategoryOutput struct {
	Category string `json:"category" jsonschema:"Category identifier"`
	Added    int    `json:"added"`
	Changed  int    `json:"changed"`
	Removed  int    `json:"removed"`
}

// InstanceSyncOutput is the structured output for pihole_instance_sync.
type InstanceSyncOutput struct {
	Source       string            `json:"source"`
	Target       string            `json:"target"`
	Mode         string            `json:"mode" jsonschema:"plan or apply"`
	InSync       bool              `json:"in_sync"`
	Prune        bool              `json:"prune"`
	ConfirmToken string            `json:"confirm_token,omitempty" jsonschema:"Token to pass back with mode=apply"`
	Plan         []pihole.PlanStep `json:"plan,omitempty"`
	SnapshotPath string            `json:"snapshot_path,omitempty" jsonschema:"Path to the target backup taken before applying"`
	Applied      int               `json:"applied,omitempty"`
	Failed       int               `json:"failed,omitempty"`
	Ops          []pihole.OpResult `json:"ops,omitempty"`
}

// RegisterSync registers the multi-instance diff and sync tools. They only
// exist when more than one instance is configured, and are registered directly
// (not via addTool) because they take explicit source/target arguments rather
// than the shared "instance" selector.
func RegisterSync(s *server.MCPServer, r *pihole.Registry) {
	if r.Len() < 2 {
		return
	}

	diffTool := mcp.NewTool("pihole_instance_diff",
		mcp.WithTitleAnnotation("Compare Instances"),
		mcp.WithDescription("Compare configuration between two Pi-hole instances (adlists, allow/deny rules, groups, clients, local DNS). Read-only — reports what a sync would change."),
		mcp.WithString("source", mcp.Description("Source-of-truth instance name. Default: the first configured instance.")),
		mcp.WithString("target", mcp.Required(), mcp.Description("Instance compared against the source.")),
		mcp.WithString("categories", mcp.Description(syncCategoriesDesc)),
		detailParam,
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithOutputSchema[InstanceDiffOutput](),
	)
	normaliseReadOnlyAnnotations(&diffTool)
	s.AddTool(diffTool, withTracing(diffTool.Name, instanceDiffHandler(r)))

	syncTool := mcp.NewTool("pihole_instance_sync",
		mcp.WithTitleAnnotation("Sync Instances"),
		mcp.WithDescription("Reconcile a target Pi-hole towards a source. Runs as a dry-run plan by default; re-call with mode=apply and the returned confirm_token to apply. Pushes one direction only (source to target)."),
		mcp.WithString("source", mcp.Description("Source-of-truth instance name. Default: the first configured instance.")),
		mcp.WithString("target", mcp.Required(), mcp.Description("Instance to update. Only this instance is written to.")),
		mcp.WithString("categories", mcp.Description(syncCategoriesDesc)),
		mcp.WithString("mode", mcp.Description("'plan' (default, dry-run) or 'apply'."), mcp.Enum("plan", "apply")),
		mcp.WithString("confirm_token", mcp.Description("Token returned by a plan; required for mode=apply.")),
		mcp.WithBoolean("prune", mcp.Description("Also delete target entries missing from the source (default false — add/update only).")),
		mcp.WithBoolean("snapshot", mcp.Description("Export a teleporter backup of the target before applying (default true).")),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithOutputSchema[InstanceSyncOutput](),
	)
	s.AddTool(syncTool, withTracing(syncTool.Name, instanceSyncHandler(r)))
}

// resolveSourceTarget resolves the source (default: first instance) and target
// clients, rejecting an absent or self-referential target.
func resolveSourceTarget(req mcp.CallToolRequest, r *pihole.Registry) (source, target *pihole.Client, err error) {
	target, err = r.Get(req.GetString("target", ""))
	if err != nil {
		return nil, nil, err
	}
	if name := req.GetString("source", ""); name != "" {
		source, err = r.Get(name)
		if err != nil {
			return nil, nil, err
		}
	} else {
		source = r.Default()
	}
	if source.Name() == target.Name() {
		return nil, nil, fmt.Errorf("source and target must be different instances (both are %q)", target.Name())
	}
	return source, target, nil
}

// parseSyncCategories resolves the optional categories argument to a validated
// category list, defaulting to the safe set when omitted.
func parseSyncCategories(req mcp.CallToolRequest) ([]pihole.SyncCategory, error) {
	raw := strings.TrimSpace(req.GetString("categories", ""))
	if raw == "" {
		return pihole.DefaultSyncCategories, nil
	}
	var cats []pihole.SyncCategory
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		cat, err := pihole.ParseSyncCategory(part)
		if err != nil {
			return nil, err
		}
		cats = append(cats, cat)
	}
	if len(cats) == 0 {
		return nil, fmt.Errorf("no valid categories supplied")
	}
	return cats, nil
}

func instanceDiffHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		source, target, err := resolveSourceTarget(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		cats, err := parseSyncCategories(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		diff, err := pihole.ComputeDiff(ctx, source, target, cats)
		if err != nil {
			return toolError("compare instances", err), nil
		}

		added, changed, removed := diff.Totals()
		output := InstanceDiffOutput{
			Source:  source.Name(),
			Target:  target.Name(),
			InSync:  diff.InSync(true),
			Added:   added,
			Changed: changed,
			Removed: removed,
			Plan:    diff.Plan(false),
		}
		for _, c := range diff.Counts() {
			output.Categories = append(output.Categories, DiffCategoryOutput{
				Category: c.Category, Added: c.Added, Changed: c.Changed, Removed: c.Removed,
			})
		}

		var b strings.Builder
		fmt.Fprintf(&b, "**%s → %s**\n", source.Name(), target.Name())
		if output.InSync {
			b.WriteString("Instances are in sync for the compared categories.\n")
			return mcp.NewToolResultStructured(output, b.String()), nil
		}
		fmt.Fprintf(&b, "%d to add, %d to update, %d only on target.\n", added, changed, removed)
		for _, c := range diff.Counts() {
			if c.Added+c.Changed+c.Removed == 0 {
				continue
			}
			fmt.Fprintf(&b, "- %s: +%d add, ~%d update, -%d remove\n", c.Label, c.Added, c.Changed, c.Removed)
		}
		if getDetail(req) == "full" {
			b.WriteString("\nPlan (add/update; deletes need prune):\n")
			for _, step := range diff.Plan(false) {
				fmt.Fprintf(&b, "- %s %s: %s\n", step.Action, step.Category, step.Item)
			}
		}
		b.WriteString("\nRun pihole_instance_sync to apply.")
		return mcp.NewToolResultStructured(output, b.String()), nil
	}
}

func instanceSyncHandler(r *pihole.Registry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		source, target, err := resolveSourceTarget(req, r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		cats, err := parseSyncCategories(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		prune := req.GetBool("prune", false)
		mode := req.GetString("mode", "plan")

		diff, err := pihole.ComputeDiff(ctx, source, target, cats)
		if err != nil {
			return toolError("compare instances", err), nil
		}
		token := diff.Token(prune)
		inSync := diff.InSync(prune)

		output := InstanceSyncOutput{
			Source: source.Name(),
			Target: target.Name(),
			Mode:   mode,
			InSync: inSync,
			Prune:  prune,
			Plan:   diff.Plan(prune),
		}

		if mode != "apply" {
			output.ConfirmToken = token
			var b strings.Builder
			fmt.Fprintf(&b, "**Plan: %s → %s** (prune=%v)\n", source.Name(), target.Name(), prune)
			if inSync {
				b.WriteString("Already in sync — nothing to apply.")
				return mcp.NewToolResultStructured(output, b.String()), nil
			}
			for _, step := range diff.Plan(prune) {
				fmt.Fprintf(&b, "- %s %s: %s\n", step.Action, step.Category, step.Item)
			}
			fmt.Fprintf(&b, "\nTo apply: re-run with mode=apply and confirm_token=%s", token)
			return mcp.NewToolResultStructured(output, b.String()), nil
		}

		// mode == apply.
		provided := req.GetString("confirm_token", "")
		if provided == "" {
			return mcp.NewToolResultError("mode=apply requires confirm_token from a prior plan"), nil
		}
		if provided != token {
			return mcp.NewToolResultError("confirm_token does not match the current plan — the configuration changed since planning. Re-run with mode=plan and use the new token."), nil
		}
		if inSync {
			output.ConfirmToken = ""
			return mcp.NewToolResultStructured(output, fmt.Sprintf("**%s → %s**: already in sync — nothing applied.", source.Name(), target.Name())), nil
		}

		if req.GetBool("snapshot", true) {
			path, serr := exportSnapshot(ctx, target)
			if serr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("aborting before any change: failed to snapshot target %q: %v", target.Name(), serr)), nil
			}
			output.SnapshotPath = path
		}

		res := pihole.ApplyPlan(ctx, target, diff, prune)
		output.Applied = res.Applied
		output.Failed = res.Failed
		output.Ops = res.Ops

		sendLog(ctx, mcp.LoggingLevelInfo, "sync", map[string]any{
			"event": "instance_sync", "source": source.Name(), "target": target.Name(),
			"applied": res.Applied, "failed": res.Failed, "prune": prune,
		})

		var b strings.Builder
		fmt.Fprintf(&b, "**Synced %s → %s.** %d applied, %d failed.\n", source.Name(), target.Name(), res.Applied, res.Failed)
		if output.SnapshotPath != "" {
			fmt.Fprintf(&b, "Target backup: %s\n", output.SnapshotPath)
		}
		for _, op := range res.Ops {
			if op.Err != "" {
				fmt.Fprintf(&b, "- FAILED %s %s (%s): %s\n", op.Action, op.Category, op.Item, op.Err)
			}
		}
		result := mcp.NewToolResultStructured(output, b.String())
		if res.Failed > 0 && res.Applied == 0 {
			return mcp.NewToolResultError(b.String()), nil
		}
		return result, nil
	}
}

// exportSnapshot downloads a teleporter backup of the client to a temp file and
// returns its path. Used as a rollback point before a sync applies changes.
func exportSnapshot(ctx context.Context, c *pihole.Client) (string, error) {
	resp, err := c.DoRaw(ctx, "GET", "/teleporter", nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	tmp, err := os.CreateTemp("", fmt.Sprintf("pihole-%s-snapshot-*.zip", sanitiseName(c.Name())))
	if err != nil {
		return "", err
	}
	_, err = io.Copy(tmp, resp.Body)
	if closeErr := tmp.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// sanitiseName makes an instance name safe for use in a temp file name.
func sanitiseName(name string) string {
	if name == "" {
		return "instance"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
}
