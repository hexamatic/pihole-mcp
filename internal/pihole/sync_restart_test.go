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
