package pihole

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
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
	// before is the target's compare value, set on an entry the plan updates
	// so the plan can show what it replaces.
	before string
	add    writeFunc
	update writeFunc
	delete writeFunc
}

// writeFunc makes one change on the target. restart is meaningful only for a
// category whose writes restart FTL (see restartingCategories); the rest
// ignore it.
type writeFunc func(ctx context.Context, c *Client, restart bool) error

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
	domains, err := readListField[Domain](ctx, c, "/domains", "domains")
	if err != nil {
		return nil, err
	}
	m := make(map[string]item, len(domains))
	for _, d := range domains {
		d := d
		key := strings.Join([]string{"domain", d.Type, d.Kind, d.Domain}, "|")
		base := fmt.Sprintf("/domains/%s/%s", d.Type, d.Kind)
		single := base + "/" + EscapePathSegment(d.Domain)
		m[key] = item{
			key:     key,
			label:   fmt.Sprintf("%s (%s/%s)", d.Domain, d.Type, d.Kind),
			compare: fmt.Sprintf("enabled=%v;comment=%s", d.Enabled, d.Comment),
			add: func(ctx context.Context, c *Client, _ bool) error {
				return c.Post(ctx, base, map[string]any{"domain": d.Domain, "comment": d.Comment, "enabled": d.Enabled}, nil)
			},
			update: func(ctx context.Context, c *Client, _ bool) error {
				return c.Put(ctx, single, map[string]any{"comment": d.Comment, "enabled": d.Enabled}, nil)
			},
			delete: func(ctx context.Context, c *Client, _ bool) error {
				return c.Delete(ctx, single)
			},
		}
	}
	return m, nil
}

func readLists(ctx context.Context, c *Client) (map[string]item, error) {
	lists, err := readListField[List](ctx, c, "/lists", "lists")
	if err != nil {
		return nil, err
	}
	m := make(map[string]item, len(lists))
	for _, l := range lists {
		l := l
		key := strings.Join([]string{"list", l.Type, l.Address}, "|")
		single := "/lists/" + EscapePathSegment(l.Address) + "?type=" + url.QueryEscape(l.Type)
		m[key] = item{
			key:     key,
			label:   fmt.Sprintf("%s (%s)", l.Address, l.Type),
			compare: fmt.Sprintf("enabled=%v;comment=%s", l.Enabled, l.Comment),
			add: func(ctx context.Context, c *Client, _ bool) error {
				return c.Post(ctx, "/lists?type="+url.QueryEscape(l.Type), map[string]any{"address": l.Address, "comment": l.Comment, "enabled": l.Enabled}, nil)
			},
			update: func(ctx context.Context, c *Client, _ bool) error {
				return c.Put(ctx, single, map[string]any{"comment": l.Comment, "enabled": l.Enabled, "type": l.Type}, nil)
			},
			delete: func(ctx context.Context, c *Client, _ bool) error {
				return c.Delete(ctx, single)
			},
		}
	}
	return m, nil
}

func readGroups(ctx context.Context, c *Client) (map[string]item, error) {
	groups, err := readListField[Group](ctx, c, "/groups", "groups")
	if err != nil {
		return nil, err
	}
	m := make(map[string]item, len(groups))
	for _, g := range groups {
		g := g
		key := "group|" + g.Name
		single := "/groups/" + EscapePathSegment(g.Name)
		m[key] = item{
			key:     key,
			label:   g.Name,
			compare: fmt.Sprintf("enabled=%v;comment=%s", g.Enabled, g.Comment),
			add: func(ctx context.Context, c *Client, _ bool) error {
				return c.Post(ctx, "/groups", map[string]any{"name": g.Name, "comment": g.Comment, "enabled": g.Enabled}, nil)
			},
			update: func(ctx context.Context, c *Client, _ bool) error {
				return c.Put(ctx, single, map[string]any{"comment": g.Comment, "enabled": g.Enabled}, nil)
			},
			delete: func(ctx context.Context, c *Client, _ bool) error {
				return c.Delete(ctx, single)
			},
		}
	}
	return m, nil
}

func readClients(ctx context.Context, c *Client) (map[string]item, error) {
	clients, err := readListField[ClientEntry](ctx, c, "/clients", "clients")
	if err != nil {
		return nil, err
	}
	m := make(map[string]item, len(clients))
	for _, cl := range clients {
		cl := cl
		key := "client|" + cl.Client
		single := "/clients/" + EscapePathSegment(cl.Client)
		m[key] = item{
			key:     key,
			label:   cl.Client,
			compare: "comment=" + cl.Comment,
			add: func(ctx context.Context, c *Client, _ bool) error {
				return c.Post(ctx, "/clients", map[string]any{"client": cl.Client, "comment": cl.Comment}, nil)
			},
			update: func(ctx context.Context, c *Client, _ bool) error {
				return c.Put(ctx, single, map[string]any{"comment": cl.Comment}, nil)
			},
			delete: func(ctx context.Context, c *Client, _ bool) error {
				return c.Delete(ctx, single)
			},
		}
	}
	return m, nil
}

// readListField reads the list called field from the reply to path. A
// reply without the list is an error rather than an empty list. Decoded as
// empty, a reply that lacked it, such as an empty object from a proxy, made
// a pruning sync delete every entry on the target.
func readListField[T any](ctx context.Context, c *Client, path, field string) ([]T, error) {
	var raw map[string]json.RawMessage
	if err := c.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	data, ok := raw[field]
	if !ok || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, fmt.Errorf("pi-hole's reply to %s has no %q list, so it cannot be compared", path, field)
	}
	var out []T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode the %q list from %s: %w", field, path, err)
	}
	return out, nil
}

func readLocalDNS(ctx context.Context, c *Client) (map[string]item, error) {
	return readDNSArray(ctx, c, "hosts", "host", false)
}

func readCNAMEs(ctx context.Context, c *Client) (map[string]item, error) {
	return readDNSArray(ctx, c, "cnameRecords", "cname", true)
}

// restartingCategories are the categories whose writes make FTL restart.
// dns.cnameRecords carries FLAG_RESTART_FTL in FTL v6.7 and v6.8; dns.hosts
// does not.
var restartingCategories = map[SyncCategory]bool{CategoryCNAME: true}

// categorySettings are the config settings a category's writes change, which
// a write locks so it cannot land between another writer's read of the
// setting and its write of the whole setting back (Client.LockSettings).
var categorySettings = map[SyncCategory]string{
	CategoryLocalDNS: "dns.hosts",
	CategoryCNAME:    "dns.cnameRecords",
}

// readDNSArray reads a string-array config value under the dns section
// (e.g. hosts, cnameRecords). Each entry is an opaque string: there is no
// change detection (compare is empty), only add and remove.
//
// For a field whose writes restart FTL (restarts), each write says whether
// it should: ApplyPlan lets only the last one restart.
func readDNSArray(ctx context.Context, c *Client, field, kind string, restarts bool) (map[string]item, error) {
	var resp ConfigResponse
	if err := c.Get(ctx, "/config/dns", &resp); err != nil {
		return nil, err
	}
	// FTL nests a config item under its full path from the root: a request for
	// dns.hosts comes back as {"config":{"dns":{"hosts":[...]}}}. Reading
	// resp.Config[field] read the wrapper, found nothing, and discarded the
	// error, so a sync against a real Pi-hole saw zero local DNS and CNAME
	// records and reported them as in sync.
	//
	// A reply without the array is an error rather than an empty list: read
	// as empty, it made a pruning sync delete every record on the target.
	dns, _ := resp.Config["dns"].(map[string]any)
	raw, ok := dns[field].([]any)
	if !ok {
		return nil, fmt.Errorf("pi-hole's reply to /config/dns has no dns.%s list, so it cannot be compared", field)
	}
	m := make(map[string]item, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		key := kind + "|" + s
		path := "/config/dns/" + field + "/" + EscapePathSegment(s)
		pathFor := func(restart bool) string {
			if restarts && !restart {
				return path + "?restart=false"
			}
			return path
		}
		m[key] = item{
			key:   key,
			label: s,
			add: func(ctx context.Context, c *Client, restart bool) error {
				return c.Put(ctx, pathFor(restart), nil, nil)
			},
			delete: func(ctx context.Context, c *Client, restart bool) error {
				return c.Delete(ctx, pathFor(restart))
			},
		}
	}
	return m, nil
}

// CategoryDiff holds the per-category differences between source and target.
type CategoryDiff struct {
	Category SyncCategory
	// sourceCount is how many entries the source has in this category.
	sourceCount int
	added       []item
	changed     []item
	removed     []item
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

		cd := CategoryDiff{Category: cat, sourceCount: len(src)}
		for _, k := range sortedKeys(src) {
			s := src[k]
			t, ok := tgt[k]
			switch {
			case !ok:
				cd.added = append(cd.added, s)
			case s.compare != "" && s.compare != t.compare:
				s.before = t.compare
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

// PruneWipes returns the categories in which a pruning sync would delete
// every entry on the target because the source has none. That is far more
// often a source that is new, misconfigured or answering oddly than an intent
// to empty the target, so the sync tool refuses it.
func (d *Diff) PruneWipes() []CategoryCount {
	var out []CategoryCount
	for _, c := range d.Categories {
		if c.sourceCount == 0 && len(c.removed) > 0 {
			out = append(out, CategoryCount{Category: string(c.Category), Label: categoryLabels[c.Category], Removed: len(c.removed)})
		}
	}
	return out
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
	// from and to are the entry's compared values before and after the
	// step, where it has them.
	from, to string
	run      writeFunc
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
			ops = append(ops, plannedOp{category: cat, action: "add", label: it.label, to: it.compare, run: it.add})
		}
		for _, it := range cd.changed {
			ops = append(ops, plannedOp{category: cat, action: "update", label: it.label, from: it.before, to: it.compare, run: it.update})
		}
	}
	if prune {
		for i := len(applyOrder) - 1; i >= 0; i-- {
			cd, ok := byCat[applyOrder[i]]
			if !ok {
				continue
			}
			for _, it := range cd.removed {
				ops = append(ops, plannedOp{category: applyOrder[i], action: "delete", label: it.label, from: it.compare, run: it.delete})
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
	// From and To are the entry's settings before and after the step, such
	// as "enabled=true;comment=…", where the category compares them.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// Plan returns the ordered reconciliation steps for display, without running
// them.
func (d *Diff) Plan(prune bool) []PlanStep {
	ops := d.plan(prune)
	steps := make([]PlanStep, len(ops))
	for i, op := range ops {
		steps[i] = PlanStep{Category: string(op.category), Action: op.action, Item: op.label, From: op.from, To: op.to}
	}
	return steps
}

// Token returns a deterministic confirmation token derived from the planned
// operations. apply re-derives the plan from live config and requires the same
// token, so a token mismatch means the configuration drifted since the plan was
// produced.
//
// It covers each step's values as well as its entry. Built from the entries
// alone, a token stayed valid when an entry's settings changed between plan
// and apply, so apply could write values the plan never showed.
func (d *Diff) Token(prune bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s->%s;prune=%v\n", d.Source, d.Target, prune)
	for _, op := range d.plan(prune) {
		fmt.Fprintf(&b, "%s|%s|%q|%q|%q\n", op.category, op.action, op.label, op.from, op.to)
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
	// Restarted is set when the sync restarted FTL to apply its changes and
	// FTL came back.
	Restarted bool `json:"restarted,omitempty"`
	// RestartError says why changes that need an FTL restart may not have
	// taken effect.
	RestartError string `json:"restart_error,omitempty"`
}

// ApplyPlan reconciles the target towards the source by running the planned
// operations in order. It continues past individual failures, recording each
// outcome, so a single bad entry does not abort the whole sync. Context
// cancellation stops further operations.
//
// Every CNAME write used to restart FTL, so a sync of several records
// restarted it once per record and sent each next write into the restart
// window, where FTL can apply a change and lose the reply. Now only the last
// such write asks for a restart, and the sync waits for FTL to come back
// before it goes on. If that write fails after earlier ones succeeded, the
// sync asks for the restart separately, since without one those records
// would be saved but not served. A write that lost its reply may have been
// saved, so it counts as needing the restart too.
func ApplyPlan(ctx context.Context, target *Client, d *Diff, prune bool) ApplyResult {
	ops := d.plan(prune)
	res := ApplyResult{Ops: make([]OpResult, 0, len(ops))}

	last := -1
	for i, op := range ops {
		if restartingCategories[op.category] {
			last = i
		}
	}
	awaitingRestart := false
	for i, op := range ops {
		if err := ctx.Err(); err != nil {
			res.Ops = append(res.Ops, OpResult{Category: string(op.category), Action: op.action, Item: op.label, Err: err.Error()})
			res.Failed++
			continue
		}
		restart := i == last
		or := OpResult{Category: string(op.category), Action: op.action, Item: op.label}
		sent, err := runLocked(ctx, target, op, restart)
		var uncertain *UncertainError
		mayBeSaved := err == nil || errors.As(err, &uncertain)
		if err != nil {
			or.Err = err.Error()
			res.Failed++
		} else {
			res.Applied++
		}
		res.Ops = append(res.Ops, or)
		if !restartingCategories[op.category] || !mayBeSaved {
			continue
		}
		if !restart {
			awaitingRestart = true
			continue
		}
		// The write that asked for the restart. If it lost its reply, FTL
		// may have saved it and restarted, or may have seen nothing, so wait
		// for a restart and ask for one only if none comes.
		if waitErr := target.AwaitRestart(ctx, sent); waitErr == nil {
			awaitingRestart = false
			res.recordRestart(nil)
		} else if err == nil {
			awaitingRestart = false
			res.recordRestart(waitErr)
		} else {
			awaitingRestart = true
		}
	}
	if awaitingRestart {
		res.restartAfterSync(ctx, target)
	}
	return res
}

// runLocked runs one planned write, holding the lock on the config setting
// it changes, if it changes one, and returns when the write was sent. That is
// after the lock was acquired: another writer holding it may restart FTL
// meanwhile, and a restart before the write is not the one it caused.
func runLocked(ctx context.Context, target *Client, op plannedOp, restart bool) (time.Time, error) {
	if setting, ok := categorySettings[op.category]; ok {
		defer target.LockSettings(setting)()
	}
	sent := time.Now()
	return sent, op.run(ctx, target, restart)
}

// restartAfterSync restarts FTL so that CNAME records the sync saved without
// a restart take effect. It runs even when ctx has been cancelled, since by
// then the records are saved and only the restart is missing, and it is
// bounded by its own timeout instead.
func (r *ApplyResult) restartAfterSync(ctx context.Context, target *Client) {
	wait := target.restartWait
	if wait <= 0 {
		wait = restartWaitTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*wait)
	defer cancel()

	sent := time.Now()
	if err := target.Post(ctx, "/action/restartdns", nil, nil); err != nil {
		r.RestartError = "Pi-hole saved the CNAME changes but they take effect only after FTL restarts, " +
			"and the request to restart it failed: " + err.Error()
		return
	}
	r.recordRestart(target.AwaitRestart(ctx, sent))
}

func (r *ApplyResult) recordRestart(err error) {
	if err != nil {
		r.RestartError = err.Error()
		return
	}
	r.Restarted = true
}
