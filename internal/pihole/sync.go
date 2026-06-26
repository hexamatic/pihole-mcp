package pihole

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// SyncCategory identifies a class of configuration that can be compared and
// synchronised between two Pi-hole instances.
type SyncCategory string

const (
	// CategoryGroups covers Pi-hole groups (compared by name).
	CategoryGroups SyncCategory = "groups"
	// CategoryLists covers adlist/allowlist subscriptions (by address+type).
	CategoryLists SyncCategory = "lists"
	// CategoryDomains covers allow/deny exact and regex rules.
	CategoryDomains SyncCategory = "domains"
	// CategoryClients covers configured clients (by identifier).
	CategoryClients SyncCategory = "clients"
	// CategoryLocalDNS covers local DNS A/AAAA host records (dns.hosts).
	CategoryLocalDNS SyncCategory = "local_dns"
	// CategoryCNAME covers local CNAME records (dns.cnameRecords).
	CategoryCNAME SyncCategory = "cname"
)

// DefaultSyncCategories is the safe, network-wide-policy set synced by default.
// Host-specific and identity/secret configuration (DHCP, interface bindings,
// passwords, TLS, sessions) is deliberately excluded and never synced.
//
// Group *membership* associations are not synced: Pi-hole group IDs are
// instance-local and not portable, so only entity existence, comment, and
// enabled state are reconciled. Groups themselves are synced by name.
var DefaultSyncCategories = []SyncCategory{
	CategoryGroups,
	CategoryLists,
	CategoryDomains,
	CategoryClients,
	CategoryLocalDNS,
	CategoryCNAME,
}

// applyOrder is the order categories are created/updated in, so that
// referenced entities (groups) exist before entities that may reference them.
// Deletes run in reverse.
var applyOrder = []SyncCategory{
	CategoryGroups,
	CategoryLists,
	CategoryDomains,
	CategoryClients,
	CategoryLocalDNS,
	CategoryCNAME,
}

// categoryLabels gives each category a short human-readable name.
var categoryLabels = map[SyncCategory]string{
	CategoryGroups:   "groups",
	CategoryLists:    "lists",
	CategoryDomains:  "domains",
	CategoryClients:  "clients",
	CategoryLocalDNS: "local DNS records",
	CategoryCNAME:    "CNAME records",
}

// ParseSyncCategory validates a category name supplied by a caller.
func ParseSyncCategory(name string) (SyncCategory, error) {
	c := SyncCategory(strings.TrimSpace(name))
	if _, ok := categoryReaders[c]; ok {
		return c, nil
	}
	valid := make([]string, 0, len(categoryReaders))
	for k := range categoryReaders {
		valid = append(valid, string(k))
	}
	sort.Strings(valid)
	return "", fmt.Errorf("unknown sync category %q; valid categories: %s", name, strings.Join(valid, ", "))
}

// item is one comparable configuration entry within a category. The add,
// update, and delete closures capture the data needed to reconcile this entry;
// they run against the client passed in at apply time (the target).
type item struct {
	key     string // stable cross-instance identity
	label   string // human-readable description for plan output
	compare string // canonical value for change detection ("" disables updates)
	add     func(ctx context.Context, c *Client) error
	update  func(ctx context.Context, c *Client) error
	delete  func(ctx context.Context, c *Client) error
}

// reader reads a category's entries from one instance, keyed by identity.
type reader func(ctx context.Context, c *Client) (map[string]item, error)

var categoryReaders = map[SyncCategory]reader{
	CategoryGroups:   readGroups,
	CategoryLists:    readLists,
	CategoryDomains:  readDomains,
	CategoryClients:  readClients,
	CategoryLocalDNS: readLocalDNS,
	CategoryCNAME:    readCNAMEs,
}

func readDomains(ctx context.Context, c *Client) (map[string]item, error) {
	var resp DomainsResponse
	if err := c.Get(ctx, "/domains", &resp); err != nil {
		return nil, err
	}
	m := make(map[string]item, len(resp.Domains))
	for _, d := range resp.Domains {
		d := d
		key := strings.Join([]string{"domain", d.Type, d.Kind, d.Domain}, "|")
		base := fmt.Sprintf("/domains/%s/%s", d.Type, d.Kind)
		single := base + "/" + url.PathEscape(d.Domain)
		m[key] = item{
			key:     key,
			label:   fmt.Sprintf("%s (%s/%s)", d.Domain, d.Type, d.Kind),
			compare: fmt.Sprintf("enabled=%v;comment=%s", d.Enabled, d.Comment),
			add: func(ctx context.Context, c *Client) error {
				return c.Post(ctx, base, map[string]any{"domain": d.Domain, "comment": d.Comment, "enabled": d.Enabled}, nil)
			},
			update: func(ctx context.Context, c *Client) error {
				return c.Put(ctx, single, map[string]any{"comment": d.Comment, "enabled": d.Enabled}, nil)
			},
			delete: func(ctx context.Context, c *Client) error {
				return c.Delete(ctx, single)
			},
		}
	}
	return m, nil
}

func readLists(ctx context.Context, c *Client) (map[string]item, error) {
	var resp ListsResponse
	if err := c.Get(ctx, "/lists", &resp); err != nil {
		return nil, err
	}
	m := make(map[string]item, len(resp.Lists))
	for _, l := range resp.Lists {
		l := l
		key := strings.Join([]string{"list", l.Type, l.Address}, "|")
		single := "/lists/" + url.PathEscape(l.Address) + "?type=" + url.QueryEscape(l.Type)
		m[key] = item{
			key:     key,
			label:   fmt.Sprintf("%s (%s)", l.Address, l.Type),
			compare: fmt.Sprintf("enabled=%v;comment=%s", l.Enabled, l.Comment),
			add: func(ctx context.Context, c *Client) error {
				return c.Post(ctx, "/lists?type="+url.QueryEscape(l.Type), map[string]any{"address": l.Address, "comment": l.Comment, "enabled": l.Enabled}, nil)
			},
			update: func(ctx context.Context, c *Client) error {
				return c.Put(ctx, single, map[string]any{"comment": l.Comment, "enabled": l.Enabled, "type": l.Type}, nil)
			},
			delete: func(ctx context.Context, c *Client) error {
				return c.Delete(ctx, single)
			},
		}
	}
	return m, nil
}

func readGroups(ctx context.Context, c *Client) (map[string]item, error) {
	var resp GroupsResponse
	if err := c.Get(ctx, "/groups", &resp); err != nil {
		return nil, err
	}
	m := make(map[string]item, len(resp.Groups))
	for _, g := range resp.Groups {
		g := g
		key := "group|" + g.Name
		single := "/groups/" + url.PathEscape(g.Name)
		m[key] = item{
			key:     key,
			label:   g.Name,
			compare: fmt.Sprintf("enabled=%v;comment=%s", g.Enabled, g.Comment),
			add: func(ctx context.Context, c *Client) error {
				return c.Post(ctx, "/groups", map[string]any{"name": g.Name, "comment": g.Comment, "enabled": g.Enabled}, nil)
			},
			update: func(ctx context.Context, c *Client) error {
				return c.Put(ctx, single, map[string]any{"comment": g.Comment, "enabled": g.Enabled}, nil)
			},
			delete: func(ctx context.Context, c *Client) error {
				return c.Delete(ctx, single)
			},
		}
	}
	return m, nil
}

func readClients(ctx context.Context, c *Client) (map[string]item, error) {
	var resp ClientsResponse
	if err := c.Get(ctx, "/clients", &resp); err != nil {
		return nil, err
	}
	m := make(map[string]item, len(resp.Clients))
	for _, cl := range resp.Clients {
		cl := cl
		key := "client|" + cl.Client
		single := "/clients/" + url.PathEscape(cl.Client)
		m[key] = item{
			key:     key,
			label:   cl.Client,
			compare: "comment=" + cl.Comment,
			add: func(ctx context.Context, c *Client) error {
				return c.Post(ctx, "/clients", map[string]any{"client": cl.Client, "comment": cl.Comment}, nil)
			},
			update: func(ctx context.Context, c *Client) error {
				return c.Put(ctx, single, map[string]any{"comment": cl.Comment}, nil)
			},
			delete: func(ctx context.Context, c *Client) error {
				return c.Delete(ctx, single)
			},
		}
	}
	return m, nil
}

func readLocalDNS(ctx context.Context, c *Client) (map[string]item, error) {
	return readDNSArray(ctx, c, "hosts", "host")
}

func readCNAMEs(ctx context.Context, c *Client) (map[string]item, error) {
	return readDNSArray(ctx, c, "cnameRecords", "cname")
}

// readDNSArray reads a string-array config value under the dns section
// (e.g. hosts, cnameRecords). Each entry is an opaque string: there is no
// change detection (compare is empty), only add and remove.
func readDNSArray(ctx context.Context, c *Client, field, kind string) (map[string]item, error) {
	var resp ConfigResponse
	if err := c.Get(ctx, "/config/dns", &resp); err != nil {
		return nil, err
	}
	raw, _ := resp.Config[field].([]any)
	m := make(map[string]item, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		key := kind + "|" + s
		path := "/config/dns/" + field + "/" + url.PathEscape(s)
		m[key] = item{
			key:   key,
			label: s,
			add: func(ctx context.Context, c *Client) error {
				return c.Put(ctx, path, nil, nil)
			},
			delete: func(ctx context.Context, c *Client) error {
				return c.Delete(ctx, path)
			},
		}
	}
	return m, nil
}

// CategoryDiff holds the per-category differences between source and target.
type CategoryDiff struct {
	Category SyncCategory
	added    []item
	changed  []item
	removed  []item
}

// Diff is the full set of differences between a source and a target instance.
type Diff struct {
	Source     string
	Target     string
	Categories []CategoryDiff
}

// ComputeDiff reads every requested category from both instances and computes
// what would need to change on the target to match the source. It performs no
// writes.
func ComputeDiff(ctx context.Context, source, target *Client, cats []SyncCategory) (*Diff, error) {
	d := &Diff{Source: source.Name(), Target: target.Name()}
	for _, cat := range cats {
		rd, ok := categoryReaders[cat]
		if !ok {
			return nil, fmt.Errorf("unknown sync category %q", cat)
		}
		src, err := rd(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("read %s from %s: %w", cat, source.Name(), err)
		}
		tgt, err := rd(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("read %s from %s: %w", cat, target.Name(), err)
		}

		cd := CategoryDiff{Category: cat}
		for _, k := range sortedKeys(src) {
			s := src[k]
			t, ok := tgt[k]
			switch {
			case !ok:
				cd.added = append(cd.added, s)
			case s.compare != "" && s.compare != t.compare:
				cd.changed = append(cd.changed, s)
			}
		}
		for _, k := range sortedKeys(tgt) {
			if _, ok := src[k]; !ok {
				cd.removed = append(cd.removed, tgt[k])
			}
		}
		d.Categories = append(d.Categories, cd)
	}
	return d, nil
}

func sortedKeys(m map[string]item) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CategoryCount summarises the diff for one category.
type CategoryCount struct {
	Category string
	Label    string
	Added    int
	Changed  int
	Removed  int
}

// Counts returns per-category summary counts in category order.
func (d *Diff) Counts() []CategoryCount {
	out := make([]CategoryCount, 0, len(d.Categories))
	for _, cd := range d.Categories {
		out = append(out, CategoryCount{
			Category: string(cd.Category),
			Label:    categoryLabels[cd.Category],
			Added:    len(cd.added),
			Changed:  len(cd.changed),
			Removed:  len(cd.removed),
		})
	}
	return out
}

// Totals returns the aggregate add/change/remove counts across all categories.
func (d *Diff) Totals() (added, changed, removed int) {
	for _, cd := range d.Categories {
		added += len(cd.added)
		changed += len(cd.changed)
		removed += len(cd.removed)
	}
	return added, changed, removed
}

// InSync reports whether the target already matches the source for every
// compared category (ignoring removals, which only matter when pruning).
func (d *Diff) InSync(prune bool) bool {
	added, changed, removed := d.Totals()
	if prune {
		return added+changed+removed == 0
	}
	return added+changed == 0
}

// plannedOp is a single reconciliation step.
type plannedOp struct {
	category SyncCategory
	action   string // add | update | delete
	label    string
	run      func(ctx context.Context, c *Client) error
}

// plan builds the ordered list of operations needed to bring the target into
// line with the source. Adds and updates run in applyOrder; deletes (only when
// prune is set) run in reverse so dependants are removed before their groups.
func (d *Diff) plan(prune bool) []plannedOp {
	byCat := make(map[SyncCategory]CategoryDiff, len(d.Categories))
	for _, cd := range d.Categories {
		byCat[cd.Category] = cd
	}

	var ops []plannedOp
	for _, cat := range applyOrder {
		cd, ok := byCat[cat]
		if !ok {
			continue
		}
		for _, it := range cd.added {
			ops = append(ops, plannedOp{cat, "add", it.label, it.add})
		}
		for _, it := range cd.changed {
			ops = append(ops, plannedOp{cat, "update", it.label, it.update})
		}
	}
	if prune {
		for i := len(applyOrder) - 1; i >= 0; i-- {
			cd, ok := byCat[applyOrder[i]]
			if !ok {
				continue
			}
			for _, it := range cd.removed {
				ops = append(ops, plannedOp{applyOrder[i], "delete", it.label, it.delete})
			}
		}
	}
	return ops
}

// PlanStep describes one reconciliation step for display.
type PlanStep struct {
	Category string `json:"category"`
	Action   string `json:"action"`
	Item     string `json:"item"`
}

// Plan returns the ordered reconciliation steps for display, without running
// them.
func (d *Diff) Plan(prune bool) []PlanStep {
	ops := d.plan(prune)
	steps := make([]PlanStep, len(ops))
	for i, op := range ops {
		steps[i] = PlanStep{Category: string(op.category), Action: op.action, Item: op.label}
	}
	return steps
}

// Token returns a deterministic confirmation token derived from the planned
// operations. apply re-derives the plan from live config and requires the same
// token, so a token mismatch means the configuration drifted since the plan was
// produced.
func (d *Diff) Token(prune bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s->%s;prune=%v\n", d.Source, d.Target, prune)
	for _, op := range d.plan(prune) {
		fmt.Fprintf(&b, "%s|%s|%s\n", op.category, op.action, op.label)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:16]
}

// OpResult is the outcome of a single applied operation.
type OpResult struct {
	Category string `json:"category"`
	Action   string `json:"action"`
	Item     string `json:"item"`
	Err      string `json:"error,omitempty"`
}

// ApplyResult is the outcome of applying a full plan to the target.
type ApplyResult struct {
	Applied int        `json:"applied"`
	Failed  int        `json:"failed"`
	Ops     []OpResult `json:"ops"`
}

// ApplyPlan reconciles the target towards the source by running the planned
// operations in order. It continues past individual failures, recording each
// outcome, so a single bad entry does not abort the whole sync. Context
// cancellation stops further operations.
func ApplyPlan(ctx context.Context, target *Client, d *Diff, prune bool) ApplyResult {
	ops := d.plan(prune)
	res := ApplyResult{Ops: make([]OpResult, 0, len(ops))}
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			res.Ops = append(res.Ops, OpResult{Category: string(op.category), Action: op.action, Item: op.label, Err: err.Error()})
			res.Failed++
			continue
		}
		or := OpResult{Category: string(op.category), Action: op.action, Item: op.label}
		if err := op.run(ctx, target); err != nil {
			or.Err = err.Error()
			res.Failed++
		} else {
			res.Applied++
		}
		res.Ops = append(res.Ops, or)
	}
	return res
}
