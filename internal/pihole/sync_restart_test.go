package pihole_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/hexamatic/pihole-mcp/internal/pihole/wiretest"
)

// cnameSource returns a source fake holding n CNAME records and a client for it.
func cnameSource(t *testing.T, cnames ...string) *pihole.Client {
	t.Helper()
	f := piholefake.New()
	t.Cleanup(f.Close)
	f.SetCNAMEs(cnames...)
	return pihole.New(f.URL(), "test", pihole.WithName("primary"), pihole.WithRetry(0, time.Second))
}

func TestApplyPlan_RestartsFTLOncePerSync(t *testing.T) {
	// Every CNAME write used to restart FTL, so a sync of several records
	// restarted it once per record and sent each next write into the restart.
	src := cnameSource(t, "a.home,nas.home", "b.home,nas.home", "c.home,nas.home")
	tgtFake, tgt, rec := recordedTarget(t)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryCNAME})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	res := pihole.ApplyPlan(context.Background(), tgt, d, false)
	if res.Failed != 0 || res.Applied != 3 {
		t.Fatalf("ApplyPlan: applied %d failed %d (%+v)", res.Applied, res.Failed, res.Ops)
	}
	if n := tgtFake.Restarts(); n != 1 {
		t.Errorf("FTL restarted %d times for a three-record sync, want once", n)
	}
	if !res.Restarted || res.RestartError != "" {
		t.Errorf("Restarted = %v, RestartError = %q; want a confirmed restart", res.Restarted, res.RestartError)
	}

	puts := rec.Select("PUT", "")
	if len(puts) != 3 {
		t.Fatalf("expected 3 PUTs, got %d:\n%s", len(puts), rec.Dump())
	}
	for i, p := range puts {
		deferred := strings.Contains(p.RawQuery, "restart=false")
		if last := i == len(puts)-1; deferred == last {
			t.Errorf("PUT %d (%s?%s): only the last CNAME write should restart FTL", i, p.RawPath, p.RawQuery)
		}
	}
}

func TestApplyPlan_RequestsTheRestartWhenTheLastCNAMEWriteFails(t *testing.T) {
	// Without a restart, records written with restart=false are saved but not
	// served, so a failed final write must not leave them that way.
	src := cnameSource(t, "a.home,nas.home", "z.home,nas.home")
	tgtFake := piholefake.New()
	t.Cleanup(tgtFake.Close)
	inner := tgtFake.Handler()
	failing := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "z.home") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"key":"bad_request","message":"Invalid CNAME"}}`))
			return
		}
		inner.ServeHTTP(w, r)
	})
	rec := wiretest.Proxy(failing)
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	tgt := pihole.New(srv.URL, "test", pihole.WithName("secondary"), pihole.WithRetry(0, time.Second))

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryCNAME})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	res := pihole.ApplyPlan(context.Background(), tgt, d, false)
	if res.Applied != 1 || res.Failed != 1 {
		t.Fatalf("applied %d failed %d, want 1 and 1 (%+v)", res.Applied, res.Failed, res.Ops)
	}
	rec.AssertCount(t, "POST", "/action/restartdns", 1)
	if n := tgtFake.Restarts(); n != 1 || !res.Restarted {
		t.Errorf("restarts = %d, Restarted = %v; the applied record needs one restart", n, res.Restarted)
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

// cnameTarget is a target fake whose CNAME writes pass through intercept
// first, with a recorder in front.
func cnameTarget(t *testing.T, intercept func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool) (*piholefake.Fake, *pihole.Client, *wiretest.Recorder) {
	t.Helper()
	f := piholefake.New()
	t.Cleanup(f.Close)
	inner := f.Handler()
	rec := wiretest.Proxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && intercept(inner, w, r) {
			return
		}
		inner.ServeHTTP(w, r)
	}))
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return f, pihole.New(srv.URL, "test", pihole.WithName("secondary"), pihole.WithRetry(0, time.Second)), rec
}

// applyThenDrop applies the request to the fake and then drops the
// connection without a reply, as FTL does when it exits mid-request.
func applyThenDrop(inner http.Handler, w http.ResponseWriter, r *http.Request) {
	inner.ServeHTTP(httptest.NewRecorder(), r)
	if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func TestApplyPlan_AnUncertainCNAMEWriteStillGetsItsRestart(t *testing.T) {
	// The first record was saved but its reply was lost, and the write that
	// would have restarted FTL failed. Saved without a restart, the record
	// would not be served.
	src := cnameSource(t, "a.home,nas.home", "z.home,nas.home")
	tgtFake, tgt, rec := cnameTarget(t, func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool {
		switch {
		case strings.Contains(r.URL.Path, "a.home"):
			applyThenDrop(inner, w, r)
		case strings.Contains(r.URL.Path, "z.home"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"key":"bad_request","message":"Invalid CNAME"}}`))
		default:
			return false
		}
		return true
	})

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryCNAME})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	res := pihole.ApplyPlan(context.Background(), tgt, d, false)
	if res.Failed != 2 {
		t.Fatalf("failed %d, want both reported (%+v)", res.Failed, res.Ops)
	}
	rec.AssertCount(t, "POST", "/action/restartdns", 1)
	if n := tgtFake.Restarts(); n != 1 || !res.Restarted {
		t.Errorf("restarts = %d, Restarted = %v; the saved record needs one restart", n, res.Restarted)
	}
}

func TestApplyPlan_AnUncertainRestartingWriteWaitsForItsOwnRestart(t *testing.T) {
	// The write that asked for the restart lost its reply after FTL saved it
	// and restarted. The sync sees the restart and does not ask for another.
	src := cnameSource(t, "a.home,nas.home")
	tgtFake, tgt, rec := cnameTarget(t, func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool {
		applyThenDrop(inner, w, r)
		return true
	})

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryCNAME})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	res := pihole.ApplyPlan(context.Background(), tgt, d, false)
	rec.AssertCount(t, "POST", "/action/restartdns", 0)
	if n := tgtFake.Restarts(); n != 1 || !res.Restarted {
		t.Errorf("restarts = %d, Restarted = %v; want the write's own restart confirmed", n, res.Restarted)
	}
}

func TestApplyPlan_RestartsFTLEvenWhenCancelledPartWay(t *testing.T) {
	// The sync is cancelled after the first record was saved with
	// restart=false. That record still needs the restart, which used to be
	// sent with the cancelled context and so never went.
	src := cnameSource(t, "a.home,nas.home", "z.home,nas.home")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tgtFake, tgt, rec := cnameTarget(t, func(inner http.Handler, w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "a.home") {
			return false
		}
		inner.ServeHTTP(w, r)
		cancel()
		return true
	})

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, []pihole.SyncCategory{pihole.CategoryCNAME})
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	res := pihole.ApplyPlan(ctx, tgt, d, false)
	rec.AssertCount(t, "PUT", "/config/dns/cnameRecords/z.home,nas.home", 0)
	rec.AssertCount(t, "POST", "/action/restartdns", 1)
	if n := tgtFake.Restarts(); n != 1 || !res.Restarted {
		t.Errorf("restarts = %d, Restarted = %v, RestartError = %q; the saved record needs one restart",
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
	if n := rec.Count("PUT", ""); n != 0 {
		t.Errorf("the sync wrote %d CNAME record(s) while another writer held the setting", n)
	}
	unlock()
	if res := <-done; res.Applied != 1 {
		t.Errorf("after the lock was released the sync applied %d, want 1 (%+v)", res.Applied, res.Ops)
	}
}

func TestApplyPlan_ARestartWhileWaitingForTheLockIsNotTheSyncs(t *testing.T) {
	// Another writer holds the CNAME setting and restarts FTL while the sync
	// waits for it. The sync's own write then fails to restart FTL. Timed
	// from before the wait, the earlier restart passed for the sync's.
	src := cnameSource(t, "a.home,nas.home")
	f := piholefake.New()
	t.Cleanup(f.Close)
	inner := f.Handler()
	noRestart := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			r = r.Clone(r.Context())
			r.URL.RawQuery = "restart=false" // saved, but FTL does not restart
		}
		inner.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(noRestart)
	t.Cleanup(srv.Close)
	tgt := pihole.New(srv.URL, "test", pihole.WithRetry(0, time.Second), pihole.WithRestartWait(400*time.Millisecond))
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
