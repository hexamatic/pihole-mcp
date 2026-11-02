package pihole_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/hexamatic/pihole-mcp/internal/pihole/wiretest"
)

// cnameSource returns a source fake holding the given CNAME records and a
// client for it.
func cnameSource(t *testing.T, cnames ...string) *pihole.Client {
	t.Helper()
	f := piholefake.New()
	t.Cleanup(f.Close)
	f.SetCNAMEs(cnames...)
	return pihole.New(f.URL(), "test", pihole.WithName("primary"), pihole.WithRetry(0, time.Second))
}

// cnameTarget is a target fake, holding the given CNAME records, whose config
// writes pass through intercept first, with a recorder in front.
func cnameTarget(t *testing.T, intercept func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool, cnames ...string) (*piholefake.Fake, *pihole.Client, *wiretest.Recorder) {
	t.Helper()
	f := piholefake.New()
	t.Cleanup(f.Close)
	f.SetCNAMEs(cnames...)
	inner := f.Handler()
	rec := wiretest.Proxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && intercept != nil && intercept(inner, w, r) {
			return
		}
		inner.ServeHTTP(w, r)
	}))
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return f, pihole.New(srv.URL, "test", pihole.WithName("secondary"), pihole.WithRetry(0, time.Second), pihole.WithRestartWait(400*time.Millisecond)), rec
}

// applyThenDrop applies the request to the fake and then drops the
// connection without a reply, as FTL does when it exits mid-request.
func applyThenDrop(inner http.Handler, w http.ResponseWriter, r *http.Request) {
	inner.ServeHTTP(httptest.NewRecorder(), r)
	if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
		_ = conn.Close()
	}
}

// withoutRestart passes the write on as one FTL saves without restarting.
func withoutRestart(r *http.Request) *http.Request {
	r = r.Clone(r.Context())
	r.URL.RawQuery = "restart=false"
	return r
}

func syncCNAMEs(t *testing.T, ctx context.Context, src, tgt *pihole.Client, prune bool) pihole.ApplyResult {
	t.Helper()
	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryCNAME})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	return pihole.ApplyPlan(ctx, tgt, d, prune)
}

func TestApplyPlan_CNAMEChangesAreOneWriteAndOneRestart(t *testing.T) {
	// Each CNAME record used to be its own write, and each write restarted
	// FTL, interrupting DNS for the whole network every time.
	src := cnameSource(t, "a.home,nas.home", "b.home,nas.home", "c.home,nas.home")
	tgtFake, tgt, rec := cnameTarget(t, nil)

	res := syncCNAMEs(t, context.Background(), src, tgt, false)
	if res.Failed != 0 || res.Applied != 3 {
		t.Fatalf("applied %d failed %d (%+v)", res.Applied, res.Failed, res.Ops)
	}
	rec.Only(t, "PATCH", "/config").AssertField(t, "config.dns.cnameRecords", []any{"a.home,nas.home", "b.home,nas.home", "c.home,nas.home"})
	rec.AssertNone(t, "PUT", "")
	if n := tgtFake.Restarts(); n != 1 || !res.Restarted {
		t.Errorf("restarts = %d, Restarted = %v; want one confirmed restart", n, res.Restarted)
	}
}

func TestApplyPlan_RepointingAnAliasKeepsIt(t *testing.T) {
	// A pruning sync that pointed an alias somewhere new added the new record
	// first, which FTL refuses because dnsmasq will not take an alias twice,
	// and then deleted the old one, so the alias was gone from both.
	src := cnameSource(t, "keep.home,nas.home", "files.home,new.home")
	tgtFake, tgt, rec := cnameTarget(t, nil, "files.home,old.home", "keep.home,nas.home")

	res := syncCNAMEs(t, context.Background(), src, tgt, true)
	if res.Failed != 0 {
		t.Fatalf("failed %d (%+v)", res.Failed, res.Ops)
	}
	rec.Only(t, "PATCH", "/config").AssertField(t, "config.dns.cnameRecords", []any{"keep.home,nas.home", "files.home,new.home"})
	if got := strings.Join(tgtFake.CNAMEs(), " "); got != "keep.home,nas.home files.home,new.home" {
		t.Errorf("target CNAME records = %q", got)
	}
}

func TestApplyPlan_ARefusedCNAMEWriteChangesNothing(t *testing.T) {
	src := cnameSource(t, "a.home,nas.home", "z.home,nas.home")
	tgtFake, tgt, _ := cnameTarget(t, func(_ http.Handler, w http.ResponseWriter, r *http.Request) bool {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"key":"bad_request","message":"Invalid configuration","hint":"dnsmasq: duplicate CNAME"}}`))
		return true
	}, "old.home,nas.home")

	res := syncCNAMEs(t, context.Background(), src, tgt, true)
	if res.Applied != 0 || res.Failed != 3 {
		t.Fatalf("applied %d failed %d, want every CNAME step reported failed (%+v)", res.Applied, res.Failed, res.Ops)
	}
	if got := strings.Join(tgtFake.CNAMEs(), " "); got != "old.home,nas.home" || tgtFake.Restarts() != 0 {
		t.Errorf("records = %q, restarts = %d; a refused write must change nothing", got, tgtFake.Restarts())
	}
}

func TestApplyPlan_HostRecordsDoNotRestartFTL(t *testing.T) {
	// dns.hosts carries no restart flag, so its writes are sent as before.
	srcFake := piholefake.New()
	t.Cleanup(srcFake.Close)
	srcFake.SetHosts("10.0.0.5 nas.home", "10.0.0.6 pi.home")
	src := pihole.New(srcFake.URL(), "test", pihole.WithName("primary"), pihole.WithRetry(0, time.Second))
	tgtFake, tgt, rec := recordedTarget(t)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryLocalDNS})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	res := pihole.ApplyPlan(context.Background(), tgt, d, false)
	if res.Failed != 0 || res.Restarted || tgtFake.Restarts() != 0 {
		t.Errorf("host records: failed %d, Restarted %v, restarts %d", res.Failed, res.Restarted, tgtFake.Restarts())
	}
	rec.AssertCount(t, "GET", "/info/ftl", 0)
}

func TestApplyPlan_AnUncertainCNAMEWriteWaitsForItsOwnRestart(t *testing.T) {
	// The write lost its reply after FTL saved it and restarted. The sync
	// sees the restart and does not ask for another.
	src := cnameSource(t, "a.home,nas.home")
	tgtFake, tgt, rec := cnameTarget(t, func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool {
		applyThenDrop(inner, w, r)
		return true
	})

	res := syncCNAMEs(t, context.Background(), src, tgt, false)
	rec.AssertCount(t, "POST", "/action/restartdns", 0)
	if n := tgtFake.Restarts(); n != 1 || !res.Restarted {
		t.Errorf("restarts = %d, Restarted = %v; want the write's own restart confirmed", n, res.Restarted)
	}
}

func TestApplyPlan_AnUncertainCNAMEWriteWithoutARestartGetsOne(t *testing.T) {
	// The write lost its reply and FTL saved it without restarting. Saved
	// without a restart, the records are not served.
	src := cnameSource(t, "a.home,nas.home")
	tgtFake, tgt, rec := cnameTarget(t, func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool {
		applyThenDrop(inner, w, withoutRestart(r))
		return true
	})

	res := syncCNAMEs(t, context.Background(), src, tgt, false)
	rec.AssertCount(t, "POST", "/action/restartdns", 1)
	if n := tgtFake.Restarts(); n != 1 || !res.Restarted {
		t.Errorf("restarts = %d, Restarted = %v; the saved records need a restart", n, res.Restarted)
	}
}

func TestApplyPlan_RestartsFTLEvenWhenCancelledDuringTheWrite(t *testing.T) {
	// The sync is cancelled while its write is out, after FTL saved it
	// without restarting. The restart is still needed, and used to be sent
	// with the cancelled context, so it never went.
	src := cnameSource(t, "a.home,nas.home")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tgtFake, tgt, rec := cnameTarget(t, func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool {
		inner.ServeHTTP(httptest.NewRecorder(), withoutRestart(r))
		cancel()
		<-r.Context().Done()
		return true
	})

	res := syncCNAMEs(t, ctx, src, tgt, false)
	rec.AssertCount(t, "POST", "/action/restartdns", 1)
	if n := tgtFake.Restarts(); n != 1 || !res.Restarted {
		t.Errorf("restarts = %d, Restarted = %v, RestartError = %q; the saved records need one restart",
			n, res.Restarted, res.RestartError)
	}
}

func TestApplyPlan_DNSWritesWaitForTheSettingLock(t *testing.T) {
	// Removing one name from a local DNS record writes back the whole
	// setting it read. A sync write landing in between would be erased by
	// it, so the sync takes the same lock.
	src := cnameSource(t, "a.home,nas.home")
	_, tgt, rec := recordedTarget(t)
	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryCNAME})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	unlock := tgt.LockSettings("DNS.cnameRecords")
	done := make(chan pihole.ApplyResult)
	go func() { done <- pihole.ApplyPlan(context.Background(), tgt, d, false) }()
	time.Sleep(100 * time.Millisecond)
	if n := rec.Count("PATCH", ""); n != 0 {
		t.Errorf("the sync wrote CNAME records while another writer held the setting")
	}
	unlock()
	if res := <-done; res.Applied != 1 {
		t.Errorf("after the lock was released the sync applied %d, want 1 (%+v)", res.Applied, res.Ops)
	}
}

func TestApplyPlan_EntryWritesWaitForTheEntryLock(t *testing.T) {
	// An update reads an entry and writes it back; a sync write landing in
	// between would be undone by it, so the sync takes the update's lock.
	srcFake := piholefake.New()
	t.Cleanup(srcFake.Close)
	srcFake.AddDomain("deny", "exact", "ads.example.com", "from source", true)
	src := pihole.New(srcFake.URL(), "test", pihole.WithName("primary"), pihole.WithRetry(0, time.Second))
	tgtFake, tgt, rec := recordedTarget(t)
	tgtFake.AddDomain("deny", "exact", "ads.example.com", "old", true)
	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryDomains})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	unlock := tgt.LockEntry("/domains/deny/exact/ads.example.com")
	done := make(chan pihole.ApplyResult)
	go func() { done <- pihole.ApplyPlan(context.Background(), tgt, d, false) }()
	time.Sleep(100 * time.Millisecond)
	if n := rec.Count("PUT", ""); n != 0 {
		t.Errorf("the sync updated the rule while an update held it")
	}
	unlock()
	if res := <-done; res.Applied != 1 {
		t.Errorf("after the lock was released the sync applied %d, want 1 (%+v)", res.Applied, res.Ops)
	}
}

func TestApplyPlan_ARestartWhileWaitingForTheLockIsNotTheSyncs(t *testing.T) {
	// Another writer holds the CNAME setting and restarts FTL while the sync
	// waits for it. The sync's own write then does not restart FTL. Timed
	// from before the wait, the earlier restart passed for the sync's.
	src := cnameSource(t, "a.home,nas.home")
	f, tgt, _ := cnameTarget(t, func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool {
		inner.ServeHTTP(w, withoutRestart(r))
		return true
	})
	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryCNAME})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}

	unlock := tgt.LockSettings("dns.cnameRecords")
	done := make(chan pihole.ApplyResult)
	go func() { done <- pihole.ApplyPlan(context.Background(), tgt, d, false) }()
	time.Sleep(50 * time.Millisecond)
	other := pihole.New(f.URL(), "test", pihole.WithRetry(0, time.Second))
	if err := other.Post(context.Background(), "/action/restartdns", nil, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	unlock()

	if res := <-done; res.Restarted || res.RestartError == "" {
		t.Errorf("Restarted = %v, RestartError = %q; the restart before the sync's write is not its own",
			res.Restarted, res.RestartError)
	}
}
